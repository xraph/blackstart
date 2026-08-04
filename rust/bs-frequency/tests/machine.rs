use bs_case::Bus;
use bs_frequency::Aggregate;

/// The default this crate applies when a machine states no droop of its own
/// — `Defaults::droop_r`. A literal here, not an import of `Defaults`, so a
/// change to the production default is a visible diff in this file's
/// expected numbers rather than a silent pass-through.
const DEFAULT_DROOP_R: f64 = 0.05;

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

    assert!(Aggregate::of(&buses, &[0, 1], DEFAULT_DROOP_R).is_none());
}

/// Two identical machines aggregate to the same inertia constant, and to
/// twice the droop response.
#[test]
fn identical_machines_aggregate_to_their_common_inertia() {
    let buses = vec![
        machine("g0", 1.0, 2.0, 4.0, 0.05),
        machine("g1", 1.0, 2.0, 4.0, 0.05),
    ];

    let got = Aggregate::of(&buses, &[0, 1], DEFAULT_DROOP_R).expect("two machines with data");

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

    let got = Aggregate::of(&buses, &[0, 1], DEFAULT_DROOP_R).expect("two machines");

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

    let got = Aggregate::of(&buses, &[0], DEFAULT_DROOP_R).expect("one machine");

    assert!((got.inertia_h - 4.0).abs() < 1e-12);
    assert_eq!(got.machines, 1);
}

/// A machine with inertia but no droop of its own still contributes mass,
/// AND gets the system default governor response rather than none — a gap
/// in the data is not the same claim as "this machine has no governor",
/// and treating it as the latter would bias every island toward
/// over-predicting collapse. `p_max` = 2.0 is the rating; with the default
/// R = 0.05, this one machine's 1/R = 2.0/0.05 = 40, and dividing by its own
/// `generation_base` (2.0) gives `inverse_droop` = 20.0 — the same figure a
/// machine that explicitly states r = 0.05 would produce, which is exactly
/// the point: stating no droop must not be distinguishable, in the result,
/// from stating the default one.
///
/// What would make this fail: reverting the fix so a missing `droop_r` is
/// skipped instead of defaulted, which would bring `inverse_droop` back to
/// 0.0.
#[test]
fn a_machine_without_droop_gets_the_default_response() {
    let mut bus = Bus::generator("g0", 1.0, 1.0).with_real_limits(0.0, 2.0);
    bus.inertia_h = Some(4.0);

    let got = Aggregate::of(&[bus], &[0], DEFAULT_DROOP_R).expect("inertia without droop");

    assert!((got.inertia_h - 4.0).abs() < 1e-12);
    assert!(
        (got.inverse_droop - 20.0).abs() < 1e-9,
        "1/R = {}, want the default-droop figure 20.0",
        got.inverse_droop
    );
}

/// An explicit, non-positive droop is a caller stating something
/// nonsensical, not a gap — it is excluded, same as before this crate
/// started defaulting the missing case. Only `None` gets `DEFAULT_DROOP_R`;
/// `Some(0.0)` does not.
#[test]
fn an_explicit_non_positive_droop_is_excluded_not_defaulted() {
    let bus = machine("g0", 1.0, 2.0, 4.0, 0.0);

    let got = Aggregate::of(&[bus], &[0], DEFAULT_DROOP_R).expect("inertia present");

    assert!(
        (got.inverse_droop - 0.0).abs() < 1e-12,
        "1/R = {}",
        got.inverse_droop
    );
}

/// When a machine states no `p_max` — infinite, meaning unlimited rather
/// than large — present output stands in for its rating.
///
/// `Bus::generator` already leaves `p_max` at `f64::INFINITY` by default, so
/// this is exercised by simply not calling `with_real_limits`. `p_injection`
/// is 3.0, so the fallback rating is `|3.0| = 3.0`; with one machine,
/// `generation_base` is that rating exactly, since nothing else is summed
/// into it.
///
/// What would make this fail: the fallback using `p_max` itself (infinity)
/// instead of `p_injection.abs()`, which would leave `generation_base`
/// infinite rather than 3.0.
#[test]
fn a_machine_with_unlimited_p_max_uses_present_output_as_its_rating() {
    let mut bus = Bus::generator("g0", 3.0, 1.0);
    bus.inertia_h = Some(4.0);

    let got =
        Aggregate::of(&[bus], &[0], DEFAULT_DROOP_R).expect("inertia data with infinite p_max");

    assert!(
        (got.generation_base - 3.0).abs() < 1e-12,
        "generation_base = {}, want the present output 3.0",
        got.generation_base
    );
}

/// The same infinite-`p_max` fallback, but with zero present output: the
/// fallback rating is `|0.0| = 0.0`, which the `rating <= 0.0` guard then
/// excludes — the same treatment a machine with an explicit zero rating
/// gets. With this as the island's only machine, excluding it leaves
/// `machines == 0`, so the whole aggregate is `None`, not a `Some` carrying
/// a zero-rated machine.
///
/// What would make this fail: the guard admitting a zero rating (say, by
/// substituting a floor value instead of excluding it), which would produce
/// `Some` with a `generation_base` of 0.0 rather than `None`.
#[test]
fn a_machine_with_unlimited_p_max_and_no_output_contributes_nothing() {
    let mut bus = Bus::generator("g0", 0.0, 1.0);
    bus.inertia_h = Some(4.0);

    assert!(Aggregate::of(&[bus], &[0], DEFAULT_DROOP_R).is_none());
}

/// A load bus is not a machine, whatever data it happens to carry.
#[test]
fn load_buses_are_not_machines() {
    let mut load = Bus::load("b0", -1.0, 0.0);
    load.inertia_h = Some(4.0);

    assert!(Aggregate::of(&[load], &[0], DEFAULT_DROOP_R).is_none());
}
