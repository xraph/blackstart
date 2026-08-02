//! The DC contingency screen.
//!
//! One multiply-add per branch per contingency, no factorization. What comes
//! out is a ranking, not a verdict — see the crate documentation.

use bs_case::Case;
use bs_factors::{FactorError, Lodf, Screenability};

/// A single contingency: one branch out of service.
///
/// N-1 is one branch by definition. The type is a struct rather than a bare
/// index so that N-k — a common-mode failure taking a whole corridor, which is
/// exactly what a weather-driven risk model produces — is a field added here
/// rather than a signature change everywhere.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Contingency {
    /// Stable identifier, carried through so a result can name what failed.
    pub id: String,
    /// Index of the outaged branch.
    pub branch: usize,
}

impl Contingency {
    /// Builds a contingency for one branch.
    #[must_use]
    pub fn new(id: impl Into<String>, branch: usize) -> Self {
        Self {
            id: id.into(),
            branch,
        }
    }
}

/// Every in-service branch as its own contingency, in case order.
///
/// The default ordering, and deliberately the dullest one. A risk model
/// supplies a better order later — that is the coupling this project is
/// about — and this exists so the interface that consumes an ordering is
/// already in place before there is anything interesting to put in it.
#[must_use]
pub fn all_branches(case: &Case) -> Vec<Contingency> {
    case.branches
        .iter()
        .enumerate()
        .filter(|(_, branch)| branch.in_service)
        .map(|(index, branch)| Contingency::new(branch.id.clone(), index))
        .collect()
}

/// A branch loaded past its rating.
#[derive(Debug, Clone, PartialEq)]
pub struct Violation {
    /// Index of the overloaded branch.
    pub branch: usize,
    /// Post-contingency flow, per unit, signed in the from→to direction.
    pub flow: f64,
    /// The rating it exceeded, per unit.
    pub rating: f64,
    /// Flow magnitude over rating. Above 1.0 by construction.
    pub loading: f64,
}

/// What the screen could say about one contingency.
#[derive(Debug, Clone, PartialEq)]
pub enum ScreenOutcome {
    /// The screen ran. Carries every branch over its rating, worst first, and
    /// the worst loading seen anywhere — including branches within their
    /// rating, so that a case with no violations still ranks against others.
    Screened {
        violations: Vec<Violation>,
        worst_loading: f64,
    },
    /// The outage disconnects the network, so no linear answer exists. A real
    /// contingency that has to be simulated rather than screened, not a
    /// failure to compute.
    Radial,
    /// The branch was already out of service, so its outage is not a
    /// contingency at all.
    NotApplicable,
}

/// One contingency's screen result.
#[derive(Debug, Clone, PartialEq)]
pub struct ScreenResult {
    /// What was outaged.
    pub contingency: Contingency,
    /// What the screen found.
    pub outcome: ScreenOutcome,
}

impl ScreenResult {
    /// The worst loading seen, or `None` when the screen could not run.
    ///
    /// `None` rather than zero: a contingency that could not be screened is
    /// not a safe one, and sorting it as though it were is how the case most
    /// in need of simulation ends up at the bottom of the list.
    #[must_use]
    pub fn worst_loading(&self) -> Option<f64> {
        match &self.outcome {
            ScreenOutcome::Screened { worst_loading, .. } => Some(*worst_loading),
            ScreenOutcome::Radial | ScreenOutcome::NotApplicable => None,
        }
    }

    /// Whether the screen found at least one branch over its rating.
    #[must_use]
    pub fn has_violations(&self) -> bool {
        matches!(&self.outcome, ScreenOutcome::Screened { violations, .. } if !violations.is_empty())
    }
}

/// Screens a list of contingencies against pre-outage flows.
///
/// Results come back in the order the contingencies were given, not ranked.
/// Ranking is the caller's decision because the useful order depends on what
/// the caller is doing — verifying the worst K, reporting everything over a
/// threshold, or feeding a risk model — and a function that both computes and
/// sorts makes the second job harder without making the first easier.
///
/// # Errors
///
/// Returns [`FactorError`] only for conditions that make the whole screen
/// impossible. A per-contingency problem is reported in that contingency's
/// [`ScreenOutcome`] rather than aborting the sweep — one radial branch must
/// not cost the other nine hundred results.
///
/// # Panics
///
/// Panics if `pre_outage` is not one flow per branch.
pub fn screen(
    case: &Case,
    lodf: &Lodf,
    pre_outage: &[f64],
    contingencies: &[Contingency],
) -> Result<Vec<ScreenResult>, FactorError> {
    assert_eq!(
        pre_outage.len(),
        case.n_branch(),
        "pre-outage flows must have one entry per branch"
    );

    contingencies
        .iter()
        .map(|contingency| {
            let outcome = match lodf.screenability(contingency.branch) {
                Screenability::Radial => ScreenOutcome::Radial,
                Screenability::OutOfService => ScreenOutcome::NotApplicable,
                Screenability::Screenable => {
                    let flows = lodf.outage(contingency.branch, pre_outage)?;
                    assess(case, &flows)
                }
            };

            Ok(ScreenResult {
                contingency: contingency.clone(),
                outcome,
            })
        })
        .collect()
}

/// Turns post-contingency flows into violations and a worst loading.
fn assess(case: &Case, flows: &[f64]) -> ScreenOutcome {
    let mut violations = Vec::new();
    let mut worst_loading = 0.0_f64;

    for (branch, spec) in case.branches.iter().enumerate() {
        // An unrated branch is skipped rather than treated as infinitely
        // overloaded: no rating means the limit is unknown, not zero.
        if !spec.in_service || spec.rating <= 0.0 {
            continue;
        }

        let flow = flows[branch];
        let loading = flow.abs() / spec.rating;
        worst_loading = worst_loading.max(loading);

        if loading > 1.0 {
            violations.push(Violation {
                branch,
                flow,
                rating: spec.rating,
                loading,
            });
        }
    }

    // Worst first, so a caller that truncates keeps what matters. Ties break
    // on branch index, which is canonical, so the order is reproducible
    // instead of depending on the sort's stability guarantees.
    violations.sort_by(|a, b| {
        b.loading
            .partial_cmp(&a.loading)
            .unwrap_or(core::cmp::Ordering::Equal)
            .then(a.branch.cmp(&b.branch))
    });

    ScreenOutcome::Screened {
        violations,
        worst_loading,
    }
}

/// Orders screen results worst first, for choosing what to verify.
///
/// Contingencies the screen could not evaluate sort to the front, ahead of
/// every screened one. They are not safe — they are unknown, and a radial
/// outage that islands part of the network is usually the most consequential
/// thing on the list. Sorting them anywhere else buries exactly the cases that
/// most need a simulation.
#[must_use]
pub fn rank(mut results: Vec<ScreenResult>) -> Vec<ScreenResult> {
    results.sort_by(|a, b| {
        match (a.worst_loading(), b.worst_loading()) {
            (None, None) => core::cmp::Ordering::Equal,
            (None, Some(_)) => core::cmp::Ordering::Less,
            (Some(_), None) => core::cmp::Ordering::Greater,
            (Some(x), Some(y)) => y.partial_cmp(&x).unwrap_or(core::cmp::Ordering::Equal),
        }
        // Ties break on branch index so the ranking is reproducible rather
        // than dependent on the sort implementation.
        .then(a.contingency.branch.cmp(&b.contingency.branch))
    });

    results
}
