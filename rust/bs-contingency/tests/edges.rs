//! What the screen says when it cannot answer.
//!
//! This is where a screen most easily misleads. The most consequential
//! contingency on a list is often the one the linear model cannot evaluate at
//! all — a radial outage that islands part of the network — and the failure
//! mode is not a crash but a reassuring "no violations found" against a case
//! that was never examined.
//!
//! So: an unevaluable contingency must be distinguishable from a safe one, it
//! must not cost the results of every other contingency, and ranking must put
//! it in front rather than at the bottom.
#![allow(clippy::float_cmp)]

use bs_case::{Branch, Bus, Case};
use bs_contingency::{Contingency, ScreenOutcome, ScreenResult, all_branches, rank, screen};
use bs_factors::Lodf;
use bs_powerflow::solve_dc;
use bs_quantity::Base;

fn base() -> Base {
    Base::new(100.0, 138.0).unwrap()
}

/// A meshed network with ratings tight enough that some outages overload
/// something and others do not — a screen that reports everything or nothing
/// tests very little.
fn rated_mesh() -> Case {
    Case::new(
        "rated-mesh",
        base(),
        vec![
            Bus::slack("s", 1.0),
            Bus::load("a", -0.4, 0.0),
            Bus::load("b", -0.7, 0.0),
            Bus::generator("g", 0.3, 1.0),
        ],
        vec![
            Branch::new("s-a", 0, 1, 0.01, 0.10).with_rating(0.9),
            Branch::new("a-b", 1, 2, 0.01, 0.15).with_rating(0.6),
            Branch::new("b-g", 2, 3, 0.01, 0.12).with_rating(0.5),
            Branch::new("g-s", 3, 0, 0.01, 0.08).with_rating(0.9),
            Branch::new("a-g", 1, 3, 0.01, 0.20).with_rating(0.4),
        ],
    )
}

/// A radial branch cannot be screened, and must not be reported as safe. This
/// is the case where a screen most easily misleads: the most consequential
/// contingency on the list is the one the linear model cannot evaluate.
#[test]
fn an_unscreenable_contingency_is_not_reported_as_safe() {
    // Branch 2 is the only path to bus 2, so its outage islands that bus.
    let case = Case::new(
        "radial-tail",
        base(),
        vec![
            Bus::slack("s", 1.0),
            Bus::load("a", -0.2, 0.0),
            Bus::load("b", -0.5, 0.0),
        ],
        vec![
            Branch::new("s-a", 0, 1, 0.0, 0.1).with_rating(1.0),
            Branch::new("s-a-2", 0, 1, 0.0, 0.2).with_rating(1.0),
            Branch::new("a-b", 1, 2, 0.0, 0.1).with_rating(1.0),
        ],
    );

    let pre = solve_dc(&case).unwrap();
    let lodf = Lodf::build(&case).unwrap();

    let results = screen(&case, &lodf, &pre.flows, &all_branches(&case)).unwrap();

    let radial = &results[2];
    assert_eq!(radial.outcome, ScreenOutcome::Radial);
    assert!(!radial.has_violations());

    // The distinction that matters: it has no loading rather than a loading of
    // zero, so it cannot be sorted as though it were the safest case here.
    assert_eq!(radial.worst_loading(), None);

    // One radial branch must not cost the other results.
    assert_eq!(results.len(), 3);
    assert!(matches!(results[0].outcome, ScreenOutcome::Screened { .. }));
    assert!(matches!(results[1].outcome, ScreenOutcome::Screened { .. }));
}

/// Ranking puts unscreenable contingencies ahead of everything, then the worst
/// loading first. A radial outage that islands part of the network is usually
/// the most consequential thing on the list, and sorting it as "no loading
/// found" would bury it.
#[test]
fn ranking_puts_the_unknown_before_the_merely_loaded() {
    let case = Case::new(
        "radial-tail",
        base(),
        vec![
            Bus::slack("s", 1.0),
            Bus::load("a", -0.2, 0.0),
            Bus::load("b", -0.5, 0.0),
        ],
        vec![
            Branch::new("s-a", 0, 1, 0.0, 0.1).with_rating(1.0),
            Branch::new("s-a-2", 0, 1, 0.0, 0.2).with_rating(1.0),
            Branch::new("a-b", 1, 2, 0.0, 0.1).with_rating(1.0),
        ],
    );

    let pre = solve_dc(&case).unwrap();
    let lodf = Lodf::build(&case).unwrap();
    let ranked = rank(screen(&case, &lodf, &pre.flows, &all_branches(&case)).unwrap());

    assert_eq!(
        ranked[0].outcome,
        ScreenOutcome::Radial,
        "the contingency nobody can evaluate must not be at the bottom"
    );

    // The rest descend by worst loading.
    let loadings: Vec<f64> = ranked[1..]
        .iter()
        .filter_map(ScreenResult::worst_loading)
        .collect();
    for pair in loadings.windows(2) {
        assert!(pair[0] >= pair[1], "{loadings:?} is not descending");
    }
}

/// An already-open branch is not a contingency, and is distinguished from a
/// radial one because the two call for completely different responses.
#[test]
fn an_open_branch_is_excluded_and_named_separately() {
    let mut case = rated_mesh();
    case.branches[4].in_service = false;

    let pre = solve_dc(&case).unwrap();
    let lodf = Lodf::build(&case).unwrap();

    // all_branches skips it entirely — it is not a contingency to consider.
    let listed = all_branches(&case);
    assert_eq!(listed.len(), case.n_branch() - 1);
    assert!(listed.iter().all(|c| c.branch != 4));

    // Asked about it explicitly, the screen says so rather than guessing.
    let forced = screen(&case, &lodf, &pre.flows, &[Contingency::new("gone", 4)]).unwrap();
    assert_eq!(forced[0].outcome, ScreenOutcome::NotApplicable);
}

/// Unrated branches are skipped rather than reported as infinitely overloaded:
/// no rating means the limit is unknown, not zero.
#[test]
fn unrated_branches_are_never_violations() {
    let mut case = rated_mesh();
    for branch in &mut case.branches {
        branch.rating = 0.0;
    }

    let pre = solve_dc(&case).unwrap();
    let lodf = Lodf::build(&case).unwrap();

    for result in screen(&case, &lodf, &pre.flows, &all_branches(&case)).unwrap() {
        assert!(!result.has_violations(), "an unrated branch was flagged");
        assert_eq!(result.worst_loading(), Some(0.0));
    }
}
