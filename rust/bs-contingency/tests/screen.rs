//! The DC contingency screen.
//!
//! The screen's whole justification is that it agrees with a full re-solve, so
//! that is checked directly: for every branch, what the screen reports as
//! overloaded must be exactly what re-solving the network without that branch
//! shows as overloaded. No reference tool, no golden file — one path through
//! the repository against another.
//!
//! What the screen says when it cannot answer is in `edges.rs`.
//!
//! The one exact float comparison checks that a reported rating is the case's
//! rating verbatim, copied rather than recomputed — so equality is the claim.
#![allow(clippy::float_cmp)]

use bs_case::{Branch, Bus, Case};
use bs_contingency::{ScreenOutcome, ScreenResult, all_branches, screen};
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

/// The check the two-tier design rests on. For every contingency the screen
/// evaluated, the branches it calls overloaded must be exactly the branches a
/// full re-solve calls overloaded.
#[test]
fn the_screen_agrees_with_a_full_resolve() {
    let case = rated_mesh();
    let pre = solve_dc(&case).unwrap();
    let lodf = Lodf::build(&case).unwrap();

    let results = screen(&case, &lodf, &pre.flows, &all_branches(&case), 1.0).unwrap();
    assert_eq!(results.len(), case.n_branch());

    let mut ever_violated = false;

    for result in &results {
        let ScreenOutcome::Screened { violations, .. } = &result.outcome else {
            continue;
        };

        let mut opened = case.clone();
        opened.branches[result.contingency.branch].in_service = false;
        let resolved = solve_dc(&opened).unwrap();

        let mut expected: Vec<usize> = resolved
            .violations(&opened, 1.0)
            .into_iter()
            .map(|(branch, _)| branch)
            .collect();
        expected.sort_unstable();

        let mut screened: Vec<usize> = violations.iter().map(|v| v.branch).collect();
        screened.sort_unstable();

        assert_eq!(
            screened, expected,
            "outage of branch {}: the screen says {screened:?} is overloaded, \
             a re-solve says {expected:?}",
            result.contingency.branch
        );

        ever_violated |= !violations.is_empty();
    }

    assert!(
        ever_violated,
        "no contingency overloaded anything; the ratings are too loose for \
         this test to prove the screen finds violations"
    );
}

/// The reported loading must be the real one, not merely the right branch.
#[test]
fn the_reported_loading_is_the_flow_over_the_rating() {
    let case = rated_mesh();
    let pre = solve_dc(&case).unwrap();
    let lodf = Lodf::build(&case).unwrap();

    let results = screen(&case, &lodf, &pre.flows, &all_branches(&case), 1.0).unwrap();

    for result in &results {
        let ScreenOutcome::Screened { violations, .. } = &result.outcome else {
            continue;
        };

        for violation in violations {
            let expected = violation.flow.abs() / violation.rating;
            assert!(
                (violation.loading - expected).abs() < 1e-12,
                "loading {} does not match flow {} over rating {}",
                violation.loading,
                violation.flow,
                violation.rating
            );
            assert!(violation.loading > 1.0, "a violation under its rating");
            assert_eq!(violation.rating, case.branches[violation.branch].rating);
        }
    }
}

/// Violations come back worst first so a caller that truncates keeps what
/// matters.
#[test]
fn violations_are_ordered_worst_first() {
    let case = rated_mesh();
    let pre = solve_dc(&case).unwrap();
    let lodf = Lodf::build(&case).unwrap();

    for result in screen(&case, &lodf, &pre.flows, &all_branches(&case), 1.0).unwrap() {
        let ScreenOutcome::Screened { violations, .. } = &result.outcome else {
            continue;
        };

        for pair in violations.windows(2) {
            assert!(
                pair[0].loading >= pair[1].loading,
                "branch {} at {} came before branch {} at {}",
                pair[0].branch,
                pair[0].loading,
                pair[1].branch,
                pair[1].loading
            );
        }
    }
}

/// The contingency's identifier must survive into the result, or a report
/// names branch indices at an operator who thinks in line names.
#[test]
fn results_carry_the_contingency_they_came_from() {
    let case = rated_mesh();
    let pre = solve_dc(&case).unwrap();
    let lodf = Lodf::build(&case).unwrap();

    let contingencies = all_branches(&case);
    let results = screen(&case, &lodf, &pre.flows, &contingencies, 1.0).unwrap();

    for (result, contingency) in results.iter().zip(&contingencies) {
        assert_eq!(&result.contingency, contingency);
        assert_eq!(result.contingency.id, case.branches[contingency.branch].id);
    }
}

#[test]
fn repeated_screens_are_bit_identical() {
    let case = rated_mesh();
    let pre = solve_dc(&case).unwrap();
    let lodf = Lodf::build(&case).unwrap();
    let contingencies = all_branches(&case);

    let first = screen(&case, &lodf, &pre.flows, &contingencies, 1.0).unwrap();

    for _ in 0..10 {
        assert_eq!(
            first,
            screen(&case, &lodf, &pre.flows, &contingencies, 1.0).unwrap()
        );
    }
}

/// The caller's threshold has to change what comes back.
///
/// It did not. `assess` hardcoded 1.0 while the service filtered the result
/// again by the request's value, so a threshold below the rating could only
/// ever remove violations and never add them — and nothing said so. Every
/// existing test passed, because every one of them used the default.
#[test]
fn a_lower_threshold_reports_more() {
    let case = rated_mesh();
    let pre = solve_dc(&case).unwrap();
    let lodf = Lodf::build(&case).unwrap();
    let outages = all_branches(&case);

    let at_rating = count_violations(&screen(&case, &lodf, &pre.flows, &outages, 1.0).unwrap());
    let at_half = count_violations(&screen(&case, &lodf, &pre.flows, &outages, 0.5).unwrap());
    let at_double = count_violations(&screen(&case, &lodf, &pre.flows, &outages, 2.0).unwrap());

    assert!(
        at_half > at_rating,
        "halving the threshold reported {at_half} violations, not more than the \
         {at_rating} at the rating — the threshold is being ignored"
    );

    assert!(
        at_double <= at_rating,
        "doubling the threshold reported {at_double}, more than the {at_rating} \
         at the rating"
    );
}

/// Counts violations across a whole sweep.
fn count_violations(results: &[ScreenResult]) -> usize {
    results
        .iter()
        .map(|result| match &result.outcome {
            ScreenOutcome::Screened { violations, .. } => violations.len(),
            _ => 0,
        })
        .sum()
}
