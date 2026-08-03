use bs_case::Bus;
use bs_frequency::Aggregate;

fn machine(id: &str, output: f64, p_max: f64, h: f64, r: f64) -> Bus {
    Bus::generator(id, output, 1.0)
        .with_real_limits(0.0, p_max)
        .with_machine(h, r)
}

/// An island with no machine carrying inertia data has no frequency model.
///
/// `None` rather than a zero-inertia aggregate: zero inertia says the island
/// collapses on any imbalance, which is a confident claim about a network
/// nobody supplied data for.
#[test]
fn an_island_without_machine_data_has_no_aggregate() {
    let buses = vec![Bus::load("b0", -1.0, 0.0), Bus::load("b1", -1.0, 0.0)];

    assert!(Aggregate::of(&buses, &[0, 1]).is_none());
}

/// Two identical machines aggregate to the same inertia constant, and to
/// twice the droop response.
#[test]
fn identical_machines_aggregate_to_their_common_inertia() {
    let buses = vec![
        machine("g0", 1.0, 2.0, 4.0, 0.05),
        machine("g1", 1.0, 2.0, 4.0, 0.05),
    ];

    let got = Aggregate::of(&buses, &[0, 1]).expect("two machines with data");

    assert!((got.inertia_h - 4.0).abs() < 1e-12, "H = {}", got.inertia_h);
    // 1/R = (1/0.05)·(2/4) + (1/0.05)·(2/4) = 20
    assert!(
        (got.inverse_droop - 20.0).abs() < 1e-9,
        "1/R = {}",
        got.inverse_droop
    );
    assert!((got.generation_base - 4.0).abs() < 1e-12);
    assert_eq!(got.machines, 2);
}

/// Inertia is weighted by rating, so a large slow machine dominates a small
/// fast one. An unweighted mean is the easy mistake and it makes a small
/// machine as influential as the unit carrying the island.
#[test]
fn inertia_is_weighted_by_rating() {
    let buses = vec![
        machine("big", 5.0, 9.0, 5.0, 0.05),
        machine("small", 0.5, 1.0, 1.0, 0.05),
    ];

    let got = Aggregate::of(&buses, &[0, 1]).expect("two machines");

    // (5·9 + 1·1)/10 = 4.6, not the unweighted 3.0.
    assert!((got.inertia_h - 4.6).abs() < 1e-12, "H = {}", got.inertia_h);
}

/// Only the named members count. An island's aggregate must not pick up a
/// machine that separated into a different island.
#[test]
fn only_the_islands_own_members_are_aggregated() {
    let buses = vec![
        machine("mine", 1.0, 2.0, 4.0, 0.05),
        machine("theirs", 1.0, 2.0, 9.0, 0.05),
    ];

    let got = Aggregate::of(&buses, &[0]).expect("one machine");

    assert!((got.inertia_h - 4.0).abs() < 1e-12);
    assert_eq!(got.machines, 1);
}

/// A machine with inertia but no droop still contributes mass. Missing droop
/// means no governor, not no rotor.
#[test]
fn a_machine_without_droop_still_contributes_inertia() {
    let mut bus = Bus::generator("g0", 1.0, 1.0).with_real_limits(0.0, 2.0);
    bus.inertia_h = Some(4.0);

    let got = Aggregate::of(&[bus], &[0]).expect("inertia without droop");

    assert!((got.inertia_h - 4.0).abs() < 1e-12);
    assert!((got.inverse_droop - 0.0).abs() < 1e-12);
}

/// A load bus is not a machine, whatever data it happens to carry.
#[test]
fn load_buses_are_not_machines() {
    let mut load = Bus::load("b0", -1.0, 0.0);
    load.inertia_h = Some(4.0);

    assert!(Aggregate::of(&[load], &[0]).is_none());
}
