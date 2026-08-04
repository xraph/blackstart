use bs_cascade::{Islands, Subcase};
use bs_case::{Branch, Bus, BusKind, Case};
use bs_quantity::Base;

/// Two generators, two loads, one tie. Opening the tie splits it evenly.
fn split_case() -> Case {
    let base = Base::new(100e6, 345e3).expect("a valid base");
    let buses = vec![
        Bus::slack("g0", 1.0),
        Bus::load("l0", -1.0, 0.0),
        Bus::generator("g1", 1.0, 1.0),
        Bus::load("l1", -1.0, 0.0),
    ];
    let branches = vec![
        Branch::new("a", 0, 1, 0.01, 0.1).with_rating(2.0),
        Branch::new("tie", 1, 2, 0.01, 0.1).with_rating(2.0),
        Branch::new("b", 2, 3, 0.01, 0.1).with_rating(2.0),
    ];

    Case::new("split", base, buses, branches)
}

#[test]
fn a_connected_network_is_one_island() {
    let found = Islands::of(&split_case());

    assert_eq!(found.count(), 1);
    assert_eq!(found.members[0], vec![0, 1, 2, 3]);
}

/// Opening the tie gives two islands, each sorted, and the islands themselves
/// ordered by their lowest member.
#[test]
fn opening_the_tie_gives_two_ordered_islands() {
    let mut case = split_case();
    case.branches[1].in_service = false;

    let found = Islands::of(&case);

    assert_eq!(found.count(), 2);
    assert_eq!(found.members[0], vec![0, 1]);
    assert_eq!(found.members[1], vec![2, 3]);
}

/// Every bus belongs to exactly one island — none orphaned, none duplicated.
///
/// This is the invariant that catches a union-find with a missing path
/// compression or an off-by-one in the member collection, and it is cheap
/// enough to assert on every case the tests build.
#[test]
fn every_bus_belongs_to_exactly_one_island() {
    let mut case = split_case();
    case.branches[1].in_service = false;

    let found = Islands::of(&case);

    let mut seen = vec![0_usize; case.n_bus()];
    for (index, members) in found.members.iter().enumerate() {
        for &bus in members {
            seen[bus] += 1;
            assert_eq!(
                found.of_bus[bus], index,
                "bus {bus} disagrees about its island"
            );
        }
    }

    assert!(
        seen.iter().all(|&n| n == 1),
        "every bus exactly once, got {seen:?}"
    );
}

/// An isolated bus is its own island, reported rather than deleted.
///
/// The topology extension already reports de-energized sections rather than
/// dropping them, and this mirrors it: a bus that vanished from the partition
/// would take its load out of the accounting silently.
#[test]
fn an_isolated_bus_is_its_own_island() {
    let mut case = split_case();
    case.branches[0].in_service = false;
    case.branches[1].in_service = false;

    let found = Islands::of(&case);

    assert_eq!(found.count(), 3);
    assert_eq!(found.members[0], vec![0]);
}

/// The extracted subcase carries exactly one slack, whatever the island had.
#[test]
fn a_subcase_designates_exactly_one_slack() {
    let mut case = split_case();
    case.branches[1].in_service = false;

    // The island {2, 3} has a PV generator and no slack at all.
    let sub = Subcase::extract(&case, &[2, 3]).expect("an island with a machine");

    let slacks = sub
        .case
        .buses
        .iter()
        .filter(|b| b.kind == BusKind::Slack)
        .count();
    assert_eq!(slacks, 1);
    assert_eq!(sub.case.slack_index().expect("exactly one"), 0);
    assert!(
        sub.case.validate().is_ok(),
        "an extracted island must be solvable"
    );
}

/// An island with no machine cannot be solved and says so.
#[test]
fn an_island_with_no_machine_cannot_be_extracted() {
    let base = Base::new(100e6, 345e3).expect("a valid base");
    let case = Case::new(
        "dead",
        base,
        vec![Bus::load("l0", -1.0, 0.0), Bus::load("l1", -1.0, 0.0)],
        vec![Branch::new("a", 0, 1, 0.01, 0.1)],
    );

    assert!(Subcase::extract(&case, &[0, 1]).is_none());
}

/// The inverse maps round-trip: a flow computed in subcase indexing lands on
/// the right branch of the full case.
#[test]
fn flows_expand_back_onto_the_full_case() {
    let mut case = split_case();
    case.branches[1].in_service = false;

    let sub = Subcase::extract(&case, &[2, 3]).expect("an island with a machine");
    assert_eq!(sub.branch_of, vec![2]);

    let mut full = vec![0.0; case.n_branch()];
    sub.expand_flows(&[0.75], &mut full);

    assert!((full[2] - 0.75).abs() < 1e-12);
    assert!((full[0] - 0.0).abs() < 1e-12);
}

/// Only in-service branches connect. An out-of-service branch between two
/// buses does not make them one island.
#[test]
fn an_out_of_service_branch_does_not_connect() {
    let mut case = split_case();
    for branch in &mut case.branches {
        branch.in_service = false;
    }

    assert_eq!(Islands::of(&case).count(), 4);
}
