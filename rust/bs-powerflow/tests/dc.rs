//! DC power flow arithmetic, pinned against hand computation.
//!
//! Each case here has values a reader can verify with a pencil, so a
//! regression is attributable to a specific term rather than to "the answer
//! moved". The oracle-independent invariant checks live in `invariants.rs`.

use bs_case::{Branch, Bus, Case};
use bs_powerflow::solve_dc;
use bs_quantity::Base;

fn base() -> Base {
    Base::new(100.0, 138.0).unwrap()
}

fn close(got: f64, want: f64, tol: f64) -> bool {
    (got - want).abs() <= tol * want.abs().max(1.0)
}

/// Two buses, one branch. Bus 1 draws 0.5 pu through a reactance of 0.1, so
/// the angle difference is 0.5 × 0.1 = 0.05 radians and the flow is 0.5 pu.
///
/// Everything else in this file rests on this being right.
#[test]
fn two_bus_matches_hand_calculation() {
    let case = Case::new(
        "two-bus",
        base(),
        vec![Bus::slack("slack", 1.0), Bus::load("load", -0.5, 0.0)],
        vec![Branch::new("line", 0, 1, 0.01, 0.1)],
    );

    let sol = solve_dc(&case).unwrap();

    assert!(close(sol.angles[0], 0.0, 1e-12), "slack angle must be zero");
    assert!(
        close(sol.angles[1], -0.05, 1e-12),
        "angle = {}, want -0.05",
        sol.angles[1]
    );
    assert!(
        close(sol.flows[0], 0.5, 1e-12),
        "flow = {}, want 0.5 pu from slack to load",
        sol.flows[0]
    );
    assert!(close(sol.slack_injection, 0.5, 1e-12));
}

/// Three buses in a line: slack — middle — end. The end draws 1.0 pu and the
/// middle draws 0.5, so the first branch carries 1.5 and the second 1.0.
#[test]
fn three_bus_radial_flows_accumulate() {
    let case = Case::new(
        "radial",
        base(),
        vec![
            Bus::slack("s", 1.0),
            Bus::load("m", -0.5, 0.0),
            Bus::load("e", -1.0, 0.0),
        ],
        vec![
            Branch::new("s-m", 0, 1, 0.0, 0.1),
            Branch::new("m-e", 1, 2, 0.0, 0.2),
        ],
    );

    let sol = solve_dc(&case).unwrap();

    assert!(close(sol.flows[0], 1.5, 1e-12), "flow0 = {}", sol.flows[0]);
    assert!(close(sol.flows[1], 1.0, 1e-12), "flow1 = {}", sol.flows[1]);

    // θ_m = -1.5 × 0.1 = -0.15; θ_e = θ_m - 1.0 × 0.2 = -0.35
    assert!(close(sol.angles[1], -0.15, 1e-12));
    assert!(close(sol.angles[2], -0.35, 1e-12));
}

/// A loop splits flow in inverse proportion to reactance. Two parallel paths
/// of 0.1 and 0.2 carry two thirds and one third.
///
/// This is the case that would pass with a wrong sign convention on a radial
/// network and fail here, which is why it is worth having.
#[test]
fn parallel_paths_split_by_inverse_reactance() {
    let case = Case::new(
        "loop",
        base(),
        vec![
            Bus::slack("s", 1.0),
            Bus::load("a", 0.0, 0.0),
            Bus::load("b", -0.9, 0.0),
        ],
        vec![
            // Direct path: reactance 0.1
            Branch::new("s-b", 0, 2, 0.0, 0.1),
            // Indirect path: 0.1 + 0.1 = 0.2 total
            Branch::new("s-a", 0, 1, 0.0, 0.1),
            Branch::new("a-b", 1, 2, 0.0, 0.1),
        ],
    );

    let sol = solve_dc(&case).unwrap();

    assert!(close(sol.flows[0], 0.6, 1e-12), "direct = {}", sol.flows[0]);
    assert!(
        close(sol.flows[1], 0.3, 1e-12),
        "indirect = {}",
        sol.flows[1]
    );
    assert!(
        close(sol.flows[2], 0.3, 1e-12),
        "indirect = {}",
        sol.flows[2]
    );
}

#[test]
fn violations_respect_ratings() {
    // Radial, so the rated branch carries the whole load. Two branches in
    // parallel would each carry half, which is how the first draft of this
    // test managed to assert an overload that was not there.
    let case = Case::new(
        "rated",
        base(),
        vec![
            Bus::slack("s", 1.0),
            Bus::load("l", -0.9, 0.0),
            Bus::load("t", 0.0, 0.0),
        ],
        vec![
            Branch::new("rated", 0, 1, 0.0, 0.1).with_rating(0.5),
            // Unrated branches are skipped rather than reported as infinitely
            // overloaded.
            Branch::new("unrated", 1, 2, 0.0, 0.1),
        ],
    );

    let sol = solve_dc(&case).unwrap();
    let violations = sol.violations(&case, 1.0);

    assert_eq!(violations.len(), 1, "got {violations:?}");
    assert_eq!(violations[0].0, 0);
    assert!(violations[0].1 > 1.0);
}
