//! One step of a cascade: dispatch, frequency response, power flow, and the
//! earliest protection to operate.
//!
//! This is where `island`, `dispatch`, `bs_frequency` and `bs_protection`
//! meet. The order they run in is not an implementation detail — it is the
//! physical story of what one step of a cascade is:
//!
//! 1. [`Islands::of`] finds what the network has fallen apart into.
//! 2. Each island dispatches what headroom its own generators have, closing
//!    as much of its gap as it can. This runs *first* because it decides the
//!    residual imbalance the frequency model is handed next — running it
//!    after the frequency integration would integrate a trajectory against a
//!    gap dispatch was about to close, and every island would under-shed.
//! 3. Each island with a machine gets a frequency response to whatever gap
//!    dispatch left behind, and that response's shed sheds load pro-rata
//!    across the island's PQ buses before anything is solved. An island with
//!    no machine gets `None` — no rotating mass means no frequency to report,
//!    not a frequency of zero, which would claim a stiffness that isn't
//!    there.
//! 4. Each island solves its own DC power flow and contributes its flows to
//!    one full-case vector. A de-energized island (no PV or slack member)
//!    contributes zeros rather than being skipped, so a caller can always
//!    see it accounted for.
//! 5. [`bs_protection::collect`] says what protection would do against those
//!    flows.
//! 6. Protection candidates and shedding times are merged into one ordered
//!    timeline. They overlap in timescale — a zone-1 distance trip lands
//!    around 40 ms, a first UFLS stage around 300 ms — so taking "the
//!    earliest protection candidate" and "the earliest shed" as two separate
//!    minimums and comparing them after the fact is the same bug as ignoring
//!    one of them: whichever clock is computed second silently wins ties it
//!    should not, and a step can report a 40 ms trip as having happened after
//!    a 300 ms shed, which never happened. Building one `Vec` and sorting it
//!    once is what rules that out.
//! 7. Everything within the coalescing window of the earliest event fires —
//!    only protection candidates have a branch to fire, so shedding's role in
//!    the timeline is entirely to keep the window's anchor honest.
//!
//! Advisory only. See `docs/SAFETY.md`.

use std::fmt;

use bs_case::{BusKind, Case};
use bs_frequency::{Aggregate, Defaults, Model, Response, Scheme, respond};
use bs_powerflow::{PowerFlowError, solve_dc};
use bs_protection::{Protection, TripCandidate, collect};

use crate::dispatch::{Balance, Dispatchable, apply, rebalance};
use crate::island::{Islands, Subcase};

/// Tunables for one step. None of these is a network property — they are
/// choices about how the simulation resolves time and how much frequency
/// physics a caller is willing to pay for.
#[derive(Debug, Clone, PartialEq)]
pub struct Options {
    /// Trip candidates within this many seconds of the earliest event fire
    /// together. Models a breaker's operating time not being resolvable to
    /// arbitrary precision, rather than one relay operating and its
    /// near-simultaneous neighbor waiting for an entirely separate step.
    pub coalesce: f64,
    /// How far each island's frequency integration runs, seconds.
    pub horizon: f64,
    /// The frequency integration's fixed step, seconds.
    pub integration_step: f64,
    /// Turbine and load characteristics an island's machines do not state.
    pub defaults: Defaults,
}

impl Default for Options {
    fn default() -> Self {
        Self {
            coalesce: 0.1,
            horizon: 60.0,
            integration_step: 0.01,
            defaults: Defaults::default(),
        }
    }
}

/// What one step produced.
#[derive(Debug, Clone, PartialEq)]
pub struct Outcome {
    /// Trip candidates that actually fired this step, in `collect`'s
    /// canonical branch-index order.
    pub fired: Vec<TripCandidate>,
    /// The earliest event this step, across both protection and shedding.
    /// `0.0` when nothing happened — a healthy step, not an error.
    pub at: f64,
    /// The islands this step found, before any of this step's own trips.
    pub islands: Islands,
    /// One frequency response per island, in island order. `None` for an
    /// island with no machine.
    pub responses: Vec<Option<Response>>,
    /// Post-step flows, one per full-case branch. Zero for a de-energized
    /// island's branches.
    pub flows: Vec<f64>,
}

/// Why a step could not be completed.
#[derive(Debug, Clone, PartialEq)]
pub enum StepError {
    /// One island's power flow did not solve. Carries the island's index —
    /// into what would have been `Outcome::islands` — and what went wrong.
    Solve(usize, PowerFlowError),
}

impl fmt::Display for StepError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::Solve(island, error) => write!(f, "island {island}: {error}"),
        }
    }
}

impl std::error::Error for StepError {}

/// One thing that could happen first this step: a relay operating, or a
/// shedding stage crossing its threshold and holding. Both run on the same
/// clock — see the module docs — so both are candidates for the timeline
/// `step` sorts to find the earliest event. Only a trip has a branch, which
/// is why firing, in step 7, filters the protection candidates alone.
#[derive(Debug, Clone, Copy)]
enum Timing {
    Trip { at: f64, branch: usize },
    Shed { at: f64 },
}

impl Timing {
    fn at(self) -> f64 {
        match self {
            Self::Trip { at, .. } | Self::Shed { at } => at,
        }
    }

    /// Tie-break key for two events at the same instant. A shed has no
    /// branch to sort by, so it sorts after every trip at the same `at` —
    /// arbitrary, but deterministic, which is all a tie-break needs to be.
    fn tie(self) -> usize {
        match self {
            Self::Trip { branch, .. } => branch,
            Self::Shed { .. } => usize::MAX,
        }
    }
}

/// Runs one step of the cascade: islanding, dispatch, frequency response,
/// power flow, and the earliest protection to operate. See the module docs
/// for why the steps run in this order.
///
/// `case` is mutated in place: dispatch changes generator injections, a
/// shedding stage scales PQ injections, and a fired trip candidate's branch
/// is taken out of service. What comes back describes what happened, not a
/// snapshot the caller must separately apply.
///
/// # Errors
///
/// Returns [`StepError::Solve`] if any island's power flow does not solve —
/// after [`Case::validate`] has already passed for the whole case, this
/// almost always means an island has no usable slack, which
/// [`Subcase::extract`] already guards against, or the reduced system is
/// otherwise singular.
pub fn step(
    case: &mut Case,
    protection: &Protection,
    scheme: &Scheme,
    options: &Options,
) -> Result<Outcome, StepError> {
    let islands = Islands::of(case);

    // Step 2: dispatch, island by island, in canonical order. `served_load`
    // is captured here rather than recomputed later — dispatch never touches
    // load, but it is still the same `Balance` step 3's `respond` needs, and
    // computing it twice would be a second place for the two to drift apart.
    let mut residuals = Vec::with_capacity(islands.count());
    let mut served_loads = Vec::with_capacity(islands.count());

    for members in &islands.members {
        let balance = Balance::of(&case.buses, members);

        let units: Vec<Dispatchable> = members
            .iter()
            .filter_map(|&bus| {
                let spec = &case.buses[bus];
                if matches!(spec.kind, BusKind::Pv | BusKind::Slack) {
                    Some(Dispatchable {
                        bus,
                        output: spec.p_injection,
                        p_min: spec.p_min,
                        p_max: spec.p_max,
                    })
                } else {
                    None
                }
            })
            .collect();

        let plan = rebalance(balance.gap, &units);
        apply(case, &plan);

        residuals.push(plan.residual);
        served_loads.push(balance.load);
    }

    // Step 3: frequency response, per island, to whatever dispatch left
    // behind. An island with no machine gets `None` rather than a model
    // built on a zero inertia it was never told it had.
    let mut responses = Vec::with_capacity(islands.count());

    for (index, members) in islands.members.iter().enumerate() {
        let Some(aggregate) = Aggregate::of(&case.buses, members, options.defaults.droop_r) else {
            responses.push(None);
            continue;
        };

        let model = Model {
            aggregate,
            defaults: options.defaults,
        };

        let response = respond(
            &model,
            residuals[index],
            served_loads[index],
            scheme,
            options.horizon,
            options.integration_step,
        );

        // Pro-rata: every PQ bus's injection is scaled by the same factor, so
        // each bus's share of what is shed is proportional to its own size
        // rather than split evenly regardless of it.
        if response.shed_fraction > 0.0 {
            let keep = 1.0 - response.shed_fraction;
            for &bus in members {
                if case.buses[bus].kind == BusKind::Pq {
                    case.buses[bus].p_injection *= keep;
                }
            }
        }

        responses.push(Some(response));
    }

    // Step 4: each island solves on its own, against its own reference —
    // dispatch already balanced it, so which member becomes the slack cannot
    // change the answer. A de-energized island (no PV or slack member)
    // contributes zero flow on its branches rather than being skipped, so a
    // caller sees it accounted for instead of silently dropped.
    let mut flows = vec![0.0; case.n_branch()];

    for (index, members) in islands.members.iter().enumerate() {
        let Some(sub) = Subcase::extract(case, members) else {
            continue;
        };

        let solution = solve_dc(&sub.case).map_err(|error| StepError::Solve(index, error))?;
        sub.expand_flows(&solution.flows, &mut flows);
    }

    // Step 5: what protection would do against those flows.
    let candidates = collect(case, &flows, protection);

    // Step 6: one ordered timeline — see the module docs for why a merged
    // sort, not two separately computed minimums, is the thing that keeps
    // protection and shedding from being reported out of the order they
    // would actually happen in.
    let mut timeline: Vec<Timing> = candidates
        .iter()
        .map(|c| Timing::Trip {
            at: c.at,
            branch: c.branch,
        })
        .collect();

    for shed in responses.iter().flatten().flat_map(|r| r.sheds.iter()) {
        timeline.push(Timing::Shed { at: shed.at });
    }

    timeline.sort_by(|a, b| {
        a.at()
            .partial_cmp(&b.at())
            .unwrap_or(core::cmp::Ordering::Equal)
            .then(a.tie().cmp(&b.tie()))
    });

    // Step 7: nothing happening this step is a legitimate outcome — a healthy
    // network reports it as `0.0`, not an error.
    let Some(minimum) = timeline.first().map(|t| t.at()) else {
        return Ok(Outcome {
            fired: Vec::new(),
            at: 0.0,
            islands,
            responses,
            flows,
        });
    };

    let window = minimum + options.coalesce;
    let fired: Vec<TripCandidate> = candidates.into_iter().filter(|c| c.at <= window).collect();

    for candidate in &fired {
        case.branches[candidate.branch].in_service = false;
    }

    Ok(Outcome {
        fired,
        at: minimum,
        islands,
        responses,
        flows,
    })
}
