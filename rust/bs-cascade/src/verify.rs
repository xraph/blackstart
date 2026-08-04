//! Full AC verification of a cascade's surviving islands.
//!
//! [`crate::step::step`] and [`crate::simulate::simulate`] propagate a
//! cascade in the linearised DC model throughout the loop — that is what
//! makes stepping through dozens of trips and sheds affordable. DC holds
//! every voltage at 1.0 per unit by construction, so it cannot see a voltage
//! violation at all, and it cannot see a network that has no steady state to
//! find. Once a cascade has settled, been truncated, or collapsed, this
//! module runs one full AC solve per surviving island to answer the question
//! the loop itself never asked: what are the real voltages, and did anything
//! end up outside a thermal or voltage limit.
//!
//! The reasoning mirrors `bs_contingency::verify` closely — an AC solve that
//! diverges is reported as [`AcVerdict::Insolvable`], the most serious
//! outcome rather than the least, for exactly the reason `Verdict::Insolvable`
//! outranks `Secure` there: a network the power flow cannot solve may have no
//! steady state at all, and reporting that as "no violations found" inverts
//! its meaning. The verdict type is its own here rather than reused, because
//! `bs-cascade` cannot depend on `bs-contingency` — contingency screening runs
//! *before* a cascade is ever simulated, so the dependency would point
//! backwards.

use core::cmp::Ordering;

use bs_case::Case;
use bs_frequency::IslandCondition;
use bs_powerflow::ac::{
    LimitedSolution, Options, VoltageLimits, VoltageViolation, solve_with_limits,
};

use crate::dispatch::Balance;
use crate::island::{Islands, Subcase};

/// What a full AC solve found for one island.
#[derive(Debug, Clone, PartialEq)]
pub enum AcVerdict {
    /// The island solves and nothing is outside its limits.
    Secure,
    /// The island solves but something is over a limit.
    Violated {
        /// Branches over their thermal rating, full-case indexed, worst
        /// loading first.
        thermal: Vec<usize>,
        /// Buses outside the voltage band, full-case indexed.
        voltage: Vec<VoltageViolation>,
    },
    /// No AC solution exists, or there was no machine to solve one against.
    ///
    /// The most serious verdict, not the least — see the module docs. Both
    /// causes are folded into one variant on purpose: an island whose Newton
    /// iteration diverges and an island with no reference bus to solve at all
    /// are the same finding from an operator's chair — there is no steady
    /// state here — and a caller that only checks `!= Secure` must not be
    /// able to miss either one by matching on the wrong sub-case.
    Insolvable,
}

/// One island's AC verification.
#[derive(Debug, Clone, PartialEq)]
pub struct IslandReport {
    /// Full-case bus indices in this island, as [`Islands::members`] gives
    /// them — sorted, in canonical order.
    pub members: Vec<usize>,
    /// The island's frequency outcome, when one is known.
    ///
    /// Always `None` coming out of [`verify_islands`]: a bare AC solve, given
    /// only a case and a partition, carries no frequency information, and
    /// this function is never handed any. It is a distinct field from
    /// [`AcVerdict`] rather than folded into it — an island can be `Stable`
    /// on frequency and `Insolvable` on voltage at once, and that is a real
    /// combination worth being able to report, not a contradiction to
    /// collapse into a single verdict. A caller that already ran a frequency
    /// response (as [`crate::simulate::simulate`] does, one step at a time)
    /// knows more than this function does and may set it.
    pub condition: Option<IslandCondition>,
    /// What the AC solve found.
    pub verdict: AcVerdict,
    /// The island's real-power mismatch before this solve: load minus
    /// generation, positive for a shortfall, negative for a surplus — see
    /// [`Balance::gap`]. For a de-energized island this is exactly its load,
    /// which is the number an operator asks for first.
    pub balance: f64,
    /// Bus count. Reported for every island, including a de-energized one,
    /// which has no [`AcVerdict::Secure`] or [`AcVerdict::Violated`] to speak
    /// of but very much still has buses that used to be served — its bus
    /// count and lost load are the finding, not a reason to drop it from the
    /// list.
    pub bus_count: usize,
}

/// Verifies every island with a full AC solve, in the order [`Islands`]
/// gives them.
///
/// Every island gets a report, including a de-energized one — see
/// [`IslandReport::bus_count`]. An island with no PV or slack member has no
/// machine to hold a reference angle or absorb a mismatch, so there is no AC
/// solution to attempt at all; that is reported as [`AcVerdict::Insolvable`],
/// not [`AcVerdict::Secure`], because "nothing to solve" and "no steady
/// state" are the same absence from an operator's point of view, and neither
/// one is "no violations found".
#[must_use]
pub fn verify_islands(
    case: &Case,
    islands: &Islands,
    limits: VoltageLimits,
    options: Options,
) -> Vec<IslandReport> {
    islands
        .members
        .iter()
        .map(|members| verify_one(case, members, limits, options))
        .collect()
}

/// Verifies one island: extracts it, solves it, and assesses the result —
/// or, with no machine to extract a solvable sub-case around, reports
/// [`AcVerdict::Insolvable`] directly.
fn verify_one(
    case: &Case,
    members: &[usize],
    limits: VoltageLimits,
    options: Options,
) -> IslandReport {
    let balance = Balance::of(&case.buses, members).gap;
    let bus_count = members.len();

    let Some(sub) = Subcase::extract(case, members) else {
        return IslandReport {
            members: members.to_vec(),
            condition: None,
            verdict: AcVerdict::Insolvable,
            balance,
            bus_count,
        };
    };

    // Divergence is `Insolvable`, never an `Err` and never `Secure` — the
    // same reasoning `bs_contingency::verify` already applies. The error
    // detail itself is not carried here, unlike `bs-contingency`'s
    // `Verdict::Insolvable(AcError)`: this type has no dependency on
    // `bs_powerflow::ac::AcError` beyond what it already needs for
    // `solve_with_limits`, and a caller that wants the detail can call
    // `solve_with_limits` itself.
    let verdict = match solve_with_limits(&sub.case, options) {
        Ok(limited) => assess(&sub, &limited, limits),
        Err(_) => AcVerdict::Insolvable,
    };

    IslandReport {
        members: members.to_vec(),
        condition: None,
        verdict,
        balance,
        bus_count,
    }
}

/// Turns a solved sub-case into a verdict, with every index translated back
/// to the full case — a caller holding only the original `Case` must be able
/// to use a report's indices directly, without re-deriving `Subcase`'s
/// bus/branch mapping itself.
fn assess(sub: &Subcase, limited: &LimitedSolution, limits: VoltageLimits) -> AcVerdict {
    let mut thermal: Vec<(f64, usize)> = Vec::new();

    for (local, spec) in sub.case.branches.iter().enumerate() {
        // Unrated means the limit is unknown, not zero — the same convention
        // `bs_contingency::screen` uses.
        if !spec.in_service || spec.rating <= 0.0 {
            continue;
        }

        let flow = limited.solution.flows[local];
        let loading = flow.abs() / spec.rating;

        if loading > 1.0 {
            thermal.push((loading, sub.branch_of[local]));
        }
    }

    // Worst first, ties on the full-case branch index — reproducible
    // regardless of the sort's stability guarantees.
    thermal.sort_by(|a, b| {
        b.0.partial_cmp(&a.0)
            .unwrap_or(Ordering::Equal)
            .then(a.1.cmp(&b.1))
    });
    let thermal: Vec<usize> = thermal.into_iter().map(|(_, branch)| branch).collect();

    let mut voltage = Vec::new();

    for (local, &magnitude) in limited.solution.voltages.magnitude.iter().enumerate() {
        let limit = if magnitude < limits.min {
            limits.min
        } else if magnitude > limits.max {
            limits.max
        } else {
            continue;
        };

        voltage.push(VoltageViolation {
            bus: sub.bus_of[local],
            magnitude,
            limit,
        });
    }

    if thermal.is_empty() && voltage.is_empty() {
        AcVerdict::Secure
    } else {
        AcVerdict::Violated { thermal, voltage }
    }
}
