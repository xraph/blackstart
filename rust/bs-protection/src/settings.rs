//! Relay settings, and resolving them per branch.

use bs_case::Case;

use crate::distance::Distance;
use crate::overcurrent::Overcurrent;

/// Every relay guarding one branch.
#[derive(Debug, Clone, PartialEq, Default)]
pub struct Settings {
    /// The overcurrent element.
    pub overcurrent: Overcurrent,
    /// The distance element.
    pub distance: Distance,
}

/// System defaults with per-branch overrides.
///
/// Overrides are a sorted `Vec` searched by binary search rather than a map.
/// Determinism is the first reason — `clippy.toml` bans the hashing types
/// outright because their iteration order changes per process — and at the
/// handful of overrides a real request carries, the linear-cache-friendly
/// search is faster anyway.
#[derive(Debug, Clone)]
pub struct Protection {
    defaults: Settings,
    overrides: Vec<(usize, Settings)>,
}

impl Protection {
    /// Builds the resolver.
    ///
    /// A later override wins over an earlier one for the same branch, which is
    /// how the contract's `Override` list already behaves on the solve path.
    /// Two rules for the same thing would be worse than either.
    #[must_use]
    pub fn new(defaults: Settings, overrides: Vec<(usize, Settings)>) -> Self {
        let mut sorted = overrides;

        // Stable sort by branch, so equal keys keep request order and the
        // last one is the one that survives the dedup below.
        sorted.sort_by_key(|(branch, _)| *branch);
        sorted.reverse();
        sorted.dedup_by_key(|(branch, _)| *branch);
        sorted.reverse();

        Self {
            defaults,
            overrides: sorted,
        }
    }

    /// The settings guarding one branch.
    #[must_use]
    pub fn for_branch(&self, branch: usize) -> &Settings {
        match self.overrides.binary_search_by_key(&branch, |(b, _)| *b) {
            Ok(index) => &self.overrides[index].1,
            Err(_) => &self.defaults,
        }
    }
}

/// Why protection operated.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum TripCause {
    /// The overcurrent element, on its inverse-time curve or instantaneously.
    Overcurrent,
    /// A distance zone, 1-based as relay engineers number them.
    Distance { zone: usize },
}

/// One element whose protection would operate, and when.
#[derive(Debug, Clone, Copy, PartialEq)]
pub struct TripCandidate {
    /// Index of the branch.
    pub branch: usize,
    /// Seconds from now until the breaker opens.
    pub at: f64,
    /// Which element saw it.
    pub cause: TripCause,
    /// The flow that caused it, per unit, signed from→to.
    pub flow: f64,
    /// Flow magnitude over rating. Zero for an unrated branch.
    pub loading: f64,
}

/// Every element whose protection would operate against these flows.
///
/// Returned in branch-index order, which is canonical, so a caller sorting by
/// time has a reproducible tie-break available without inventing one.
///
/// **The threshold is applied here and only here.** Every branch below every
/// pickup produces no candidate at all, so there is nothing downstream to
/// filter again — which is precisely how `screen.rs` came to silently ignore a
/// caller's threshold, by collecting at one value and filtering at another.
///
/// # Panics
///
/// Panics if `flows` is not one entry per branch.
#[must_use]
pub fn collect(case: &Case, flows: &[f64], protection: &Protection) -> Vec<TripCandidate> {
    assert_eq!(
        flows.len(),
        case.n_branch(),
        "flows must have one entry per branch"
    );

    let mut candidates = Vec::new();

    for (branch, spec) in case.branches.iter().enumerate() {
        if !spec.in_service {
            continue;
        }

        let flow = flows[branch];
        let settings = protection.for_branch(branch);

        let overcurrent = settings
            .overcurrent
            .time_to_trip(flow, spec.rating)
            .map(|at| (at, TripCause::Overcurrent));

        let distance = settings
            .distance
            .time_to_trip(flow, spec.x)
            .map(|(at, zone)| (at, TripCause::Distance { zone }));

        // Whichever element is faster is the one that actually operates. Both
        // are reported through one candidate because the breaker only opens
        // once, and an event log claiming two trips of one branch is wrong.
        let soonest = match (overcurrent, distance) {
            (None, None) => continue,
            (Some(only), None) | (None, Some(only)) => only,
            (Some(a), Some(b)) => {
                if b.0 < a.0 {
                    b
                } else {
                    a
                }
            }
        };

        candidates.push(TripCandidate {
            branch,
            at: soonest.0,
            cause: soonest.1,
            flow,
            loading: if spec.rating > 0.0 {
                flow.abs() / spec.rating
            } else {
                0.0
            },
        });
    }

    candidates
}
