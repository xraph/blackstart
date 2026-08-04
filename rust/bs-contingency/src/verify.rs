//! Full AC verification of screened contingencies.
//!
//! The second tier. The DC screen ranks; this decides. A verified contingency
//! is a finding, with real voltages and real reactive power behind it, and it
//! costs a factorization per Newton iteration — which is exactly why only the
//! top of the ranking gets one.
//!
//! Verification can disagree with the screen in both directions, and both
//! matter. A DC overload that AC clears was a false positive the screen would
//! have reported; an AC voltage violation is something DC cannot see at all,
//! because DC holds every voltage at 1.0 by construction.

use bs_case::Case;
use bs_powerflow::ac::{AcError, LimitedSolution, Options, solve_with_limits};

use crate::screen::{Contingency, ScreenResult, Violation};

// Re-exported rather than defined here. Assessing an AC solution against a
// voltage band is the power-flow crate's domain, and bs-cascade needs the same
// types without needing anything else about contingency analysis. Kept
// re-exported so no existing caller has to move.
pub use bs_powerflow::ac::{VoltageLimits, VoltageViolation};

/// What a full AC solve found for one contingency.
#[derive(Debug, Clone, PartialEq)]
pub enum Verdict {
    /// The network solves and nothing is violated.
    Secure,
    /// The network solves but something is over a limit.
    Violated {
        /// Branches over their thermal rating, worst first.
        thermal: Vec<Violation>,
        /// Buses outside the voltage band.
        voltage: Vec<VoltageViolation>,
        /// Generators pinned at a reactive limit. Not a violation on its own,
        /// but the reason a voltage violation happened, and reporting the
        /// symptom without the cause sends an operator looking in the wrong
        /// place.
        binding_generators: Vec<usize>,
    },
    /// No AC solution was found.
    ///
    /// This is the most serious outcome, not the least. A contingency the
    /// power flow cannot solve is one where the network may have no steady
    /// state at all — voltage collapse — and reporting it as "no violations
    /// found" would invert its meaning entirely.
    Insolvable(AcError),
}

impl Verdict {
    /// Whether this contingency needs attention.
    ///
    /// Insolvable counts. It is the strongest signal here, and a helper that
    /// answered "no" for it would be actively dangerous.
    #[must_use]
    pub fn is_concerning(&self) -> bool {
        !matches!(self, Self::Secure)
    }
}

/// One verified contingency.
#[derive(Debug, Clone, PartialEq)]
pub struct Verification {
    /// What was outaged.
    pub contingency: Contingency,
    /// What the AC solve found.
    pub verdict: Verdict,
    /// The solution, when there was one. Kept so a caller can report actual
    /// voltages rather than only the fact that they were out of band.
    pub solution: Option<LimitedSolution>,
}

/// Verifies one contingency with a full AC solve under reactive limits.
///
/// Reactive limits are enforced rather than optional: without them a stressed
/// network reports a comfortable voltage profile no physical generator could
/// produce, and a contingency analysis is precisely the case where the
/// network is stressed.
///
/// An unsolvable case is returned as a [`Verdict`], not an `Err`. Failing to
/// converge *is* the result — it is the strongest thing this can say about a
/// contingency — and an error return would invite a caller to skip it.
#[must_use]
pub fn verify(
    case: &Case,
    contingency: &Contingency,
    limits: VoltageLimits,
    options: Options,
) -> Verification {
    let mut opened = case.clone();
    opened.branches[contingency.branch].in_service = false;

    match solve_with_limits(&opened, options) {
        Ok(limited) => {
            let verdict = assess(&opened, &limited, limits);
            Verification {
                contingency: contingency.clone(),
                verdict,
                solution: Some(limited),
            }
        }
        Err(error) => Verification {
            contingency: contingency.clone(),
            verdict: Verdict::Insolvable(error),
            solution: None,
        },
    }
}

/// Verifies the top `k` of a ranked screen.
///
/// The screen must already be ranked — this takes a prefix and does not sort,
/// so that "which contingencies were verified" is the caller's decision and is
/// visible at the call site rather than buried here.
///
/// Contingencies the screen could not evaluate are verified too, and they sort
/// to the front of a ranked list precisely so that they are.
#[must_use]
pub fn verify_top(
    case: &Case,
    ranked: &[ScreenResult],
    k: usize,
    limits: VoltageLimits,
    options: Options,
) -> Vec<Verification> {
    ranked
        .iter()
        .take(k)
        .map(|result| verify(case, &result.contingency, limits, options))
        .collect()
}

fn assess(case: &Case, limited: &LimitedSolution, limits: VoltageLimits) -> Verdict {
    let mut thermal = Vec::new();

    for (branch, spec) in case.branches.iter().enumerate() {
        // Unrated means the limit is unknown, not zero.
        if !spec.in_service || spec.rating <= 0.0 {
            continue;
        }

        let flow = limited.solution.flows[branch];
        let loading = flow.abs() / spec.rating;

        if loading > 1.0 {
            thermal.push(Violation {
                branch,
                flow,
                rating: spec.rating,
                loading,
            });
        }
    }

    thermal.sort_by(|a, b| {
        b.loading
            .partial_cmp(&a.loading)
            .unwrap_or(core::cmp::Ordering::Equal)
            .then(a.branch.cmp(&b.branch))
    });

    let mut voltage = Vec::new();

    for (bus, &magnitude) in limited.solution.voltages.magnitude.iter().enumerate() {
        let limit = if magnitude < limits.min {
            limits.min
        } else if magnitude > limits.max {
            limits.max
        } else {
            continue;
        };

        voltage.push(VoltageViolation {
            bus,
            magnitude,
            limit,
        });
    }

    let binding_generators = limited.binding();

    if thermal.is_empty() && voltage.is_empty() {
        return Verdict::Secure;
    }

    Verdict::Violated {
        thermal,
        voltage,
        binding_generators,
    }
}
