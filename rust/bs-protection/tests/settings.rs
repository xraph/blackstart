use bs_case::{Branch, Bus, Case};
use bs_protection::{Overcurrent, Protection, Settings, TripCause, collect};
use bs_quantity::Base;

fn two_branch_case() -> Case {
    let base = Base::new(100e6, 345e3).expect("a valid base");
    let buses = vec![Bus::slack("b0", 1.0), Bus::load("b1", -1.0, 0.0)];
    let branches = vec![
        Branch::new("ln0", 0, 1, 0.01, 0.1).with_rating(1.0),
        Branch::new("ln1", 0, 1, 0.01, 0.1).with_rating(1.0),
    ];

    Case::new("two", base, buses, branches)
}

/// A branch with no override gets the defaults.
#[test]
fn defaults_apply_where_nothing_overrides_them() {
    let protection = Protection::new(Settings::default(), Vec::new());

    assert!((protection.for_branch(0).overcurrent.pickup - 1.0).abs() < 1e-12);
}

/// The override wins, and only for its own branch.
#[test]
fn an_override_applies_to_exactly_one_branch() {
    let strict = Settings {
        overcurrent: Overcurrent {
            pickup: 0.5,
            ..Overcurrent::default()
        },
        ..Settings::default()
    };
    let protection = Protection::new(Settings::default(), vec![(1, strict)]);

    assert!((protection.for_branch(0).overcurrent.pickup - 1.0).abs() < 1e-12);
    assert!((protection.for_branch(1).overcurrent.pickup - 0.5).abs() < 1e-12);
}

/// Two overrides for one branch: the last wins, matching how the contract's
/// `Override` list already behaves on the solve path.
#[test]
fn a_later_override_wins_over_an_earlier_one() {
    let first = Settings {
        overcurrent: Overcurrent {
            pickup: 0.5,
            ..Overcurrent::default()
        },
        ..Settings::default()
    };
    let second = Settings {
        overcurrent: Overcurrent {
            pickup: 0.7,
            ..Overcurrent::default()
        },
        ..Settings::default()
    };
    let protection = Protection::new(Settings::default(), vec![(0, first), (0, second)]);

    assert!((protection.for_branch(0).overcurrent.pickup - 0.7).abs() < 1e-12);
}

/// Candidates come back in branch-index order, so the caller's sort is stable
/// against a canonical key rather than against collection order.
#[test]
fn candidates_are_collected_in_branch_order() {
    let case = two_branch_case();
    let protection = Protection::new(Settings::default(), Vec::new());

    let found = collect(&case, &[3.0, 3.0], &protection);

    assert_eq!(found.len(), 2);
    assert_eq!(found[0].branch, 0);
    assert_eq!(found[1].branch, 1);
}

/// An out-of-service branch carries no flow and has no protection to operate.
#[test]
fn an_out_of_service_branch_is_not_a_candidate() {
    let mut case = two_branch_case();
    case.branches[0].in_service = false;
    let protection = Protection::new(Settings::default(), Vec::new());

    let found = collect(&case, &[0.0, 3.0], &protection);

    assert_eq!(found.len(), 1);
    assert_eq!(found[0].branch, 1);
}

/// The threshold is applied once, here, where candidates are collected.
///
/// This is the screen.rs lesson. A branch below every pickup produces no
/// candidate at all, so there is nothing downstream to filter a second time
/// and therefore nothing that can silently ignore a caller's setting.
#[test]
fn a_branch_below_every_pickup_produces_no_candidate() {
    let case = two_branch_case();
    let protection = Protection::new(Settings::default(), Vec::new());

    assert!(collect(&case, &[0.5, 0.5], &protection).is_empty());
}

/// Whichever element is faster is the one that operates.
#[test]
fn the_faster_element_is_the_reported_cause() {
    let case = two_branch_case();
    let protection = Protection::new(Settings::default(), Vec::new());

    // 3.0 pu on a 1.0 rating is past the 2.5 instantaneous element (0.05 s)
    // and, on x = 0.1, past zone 1 at 12.5 pu — no. Only overcurrent sees it.
    let found = collect(&case, &[3.0, 0.0], &protection);

    assert_eq!(found[0].cause, TripCause::Overcurrent);
    assert!((found[0].loading - 3.0).abs() < 1e-12);
}
