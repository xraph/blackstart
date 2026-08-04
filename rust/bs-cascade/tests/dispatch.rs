use bs_cascade::{Balance, Dispatchable, rebalance};
use bs_case::Bus;

/// The gap is positive for a shortfall, matching bs-frequency's imbalance
/// sign. Two crates disagreeing about this sign would produce a cascade that
/// sheds load in islands with surplus generation, and it would look
/// plausible.
#[test]
fn a_short_island_has_a_positive_gap() {
    let buses = vec![Bus::generator("g0", 1.0, 1.0), Bus::load("l0", -1.6, 0.0)];

    let got = Balance::of(&buses, &[0, 1]);

    assert!((got.generation - 1.0).abs() < 1e-12);
    assert!((got.load - 1.6).abs() < 1e-12);
    assert!((got.gap - 0.6).abs() < 1e-12, "gap {}", got.gap);
}

#[test]
fn a_surplus_island_has_a_negative_gap() {
    let buses = vec![Bus::generator("g0", 2.0, 1.0), Bus::load("l0", -1.0, 0.0)];

    assert!((Balance::of(&buses, &[0, 1]).gap + 1.0).abs() < 1e-12);
}

/// An unlimited generator has unbounded headroom, which is what infinity
/// means here — not a very large number that arithmetic can accumulate.
#[test]
fn an_unlimited_generator_has_infinite_headroom() {
    let unit = Dispatchable {
        bus: 0,
        output: 1.0,
        p_min: f64::NEG_INFINITY,
        p_max: f64::INFINITY,
    };

    assert!(unit.headroom_up().is_infinite());
    assert!(unit.headroom_down().is_infinite());
}

#[test]
fn a_unit_at_its_ceiling_has_no_headroom_up() {
    let unit = Dispatchable {
        bus: 0,
        output: 1.5,
        p_min: 0.2,
        p_max: 1.5,
    };

    assert!((unit.headroom_up() - 0.0).abs() < 1e-12);
    assert!((unit.headroom_down() - 1.3).abs() < 1e-12);
}

/// A gap fully inside the fleet's headroom leaves nothing residual.
#[test]
fn a_coverable_gap_leaves_no_residual() {
    let units = vec![
        Dispatchable {
            bus: 0,
            output: 1.0,
            p_min: 0.0,
            p_max: 2.0,
        },
        Dispatchable {
            bus: 2,
            output: 1.0,
            p_min: 0.0,
            p_max: 2.0,
        },
    ];

    let plan = rebalance(0.6, &units);

    let raised: f64 = plan.changes.iter().map(|(_, delta)| delta).sum();
    assert!((raised - 0.6).abs() < 1e-9, "raised {raised}");
    assert!((plan.residual - 0.0).abs() < 1e-9);
}

/// Beyond the fleet's headroom the remainder is residual, not shed.
///
/// Dispatch closes what it can and hands the rest to the frequency model,
/// which is what decides whether load sheds. If dispatch shed directly, the
/// shedding would have no frequency behind it and every island's nadir would
/// be decorative.
#[test]
fn a_gap_beyond_headroom_leaves_a_residual() {
    let units = vec![Dispatchable {
        bus: 0,
        output: 1.0,
        p_min: 0.0,
        p_max: 1.2,
    }];

    let plan = rebalance(0.6, &units);

    let raised: f64 = plan.changes.iter().map(|(_, delta)| delta).sum();
    assert!((raised - 0.2).abs() < 1e-9, "raised {raised}");
    assert!(
        (plan.residual - 0.4).abs() < 1e-9,
        "residual {}",
        plan.residual
    );
}

/// A surplus backs generation down against `p_min`.
#[test]
fn a_surplus_backs_generation_down() {
    let units = vec![Dispatchable {
        bus: 0,
        output: 1.0,
        p_min: 0.4,
        p_max: 2.0,
    }];

    let plan = rebalance(-0.3, &units);

    assert!(
        (plan.changes[0].1 + 0.3).abs() < 1e-9,
        "got {}",
        plan.changes[0].1
    );
    assert!((plan.residual - 0.0).abs() < 1e-9);
}

/// No units means nothing dispatch can do, and the whole gap is residual.
#[test]
fn an_island_with_no_units_leaves_the_whole_gap() {
    let plan = rebalance(0.6, &[]);

    assert!(plan.changes.is_empty());
    assert!((plan.residual - 0.6).abs() < 1e-12);
}

/// Changes come back in bus order, so applying them is a canonical sequence
/// of floating-point additions rather than one that depends on unit order.
#[test]
fn changes_are_ordered_by_bus() {
    let units = vec![
        Dispatchable {
            bus: 5,
            output: 1.0,
            p_min: 0.0,
            p_max: 2.0,
        },
        Dispatchable {
            bus: 1,
            output: 1.0,
            p_min: 0.0,
            p_max: 2.0,
        },
    ];

    let plan = rebalance(0.5, &units);

    assert!(
        plan.changes.windows(2).all(|w| w[0].0 < w[1].0),
        "{:?}",
        plan.changes
    );
}

/// A zero gap changes nothing rather than producing a list of zeroes.
#[test]
fn a_balanced_island_is_left_alone() {
    let units = vec![Dispatchable {
        bus: 0,
        output: 1.0,
        p_min: 0.0,
        p_max: 2.0,
    }];

    let plan = rebalance(0.0, &units);

    assert!(plan.changes.is_empty());
    assert!((plan.residual - 0.0).abs() < 1e-12);
}

/// Pro-rata on headroom: two units with different headroom split the gap in
/// proportion to that headroom, not evenly and not by output.
#[test]
fn units_share_the_gap_in_proportion_to_their_headroom() {
    let units = vec![
        Dispatchable {
            bus: 0,
            output: 0.0,
            p_min: 0.0,
            p_max: 1.0,
        }, // headroom_up 1.0
        Dispatchable {
            bus: 1,
            output: 0.0,
            p_min: 0.0,
            p_max: 3.0,
        }, // headroom_up 3.0
    ];

    let plan = rebalance(0.4, &units);

    assert!(
        (plan.changes[0].1 - 0.1).abs() < 1e-9,
        "got {}",
        plan.changes[0].1
    );
    assert!(
        (plan.changes[1].1 - 0.3).abs() < 1e-9,
        "got {}",
        plan.changes[1].1
    );
    assert!((plan.residual - 0.0).abs() < 1e-12);
}

/// A unit with infinite headroom makes the gap fully coverable, and the
/// finite unit next to it is left alone rather than folded into the same
/// ratio.
///
/// This is the NaN path: mixing an infinite headroom with a finite one in a
/// single pro-rata ratio would compute `inf / inf` for the infinite unit's
/// own share, which is `NaN` — an untested NaN path in a numerical solver is
/// exactly the failure this codebase is built to avoid, so it gets its own
/// test rather than trusting the reasoning alone.
#[test]
fn infinite_headroom_covers_the_gap_without_poisoning_finite_units() {
    let units = vec![
        Dispatchable {
            bus: 0,
            output: 1.0,
            p_min: 0.0,
            p_max: 1.2,
        },
        Dispatchable {
            bus: 3,
            output: 1.0,
            p_min: f64::NEG_INFINITY,
            p_max: f64::INFINITY,
        },
    ];

    let plan = rebalance(5.0, &units);

    assert!(
        plan.changes.iter().all(|(_, delta)| delta.is_finite()),
        "no NaN or infinite delta: {:?}",
        plan.changes
    );
    assert_eq!(
        plan.changes.len(),
        1,
        "the finite unit is left untouched once an infinite one exists: {:?}",
        plan.changes
    );
    assert_eq!(plan.changes[0].0, 3);
    assert!((plan.changes[0].1 - 5.0).abs() < 1e-9);
    assert!((plan.residual - 0.0).abs() < 1e-12);
}

/// Two infinite-headroom units split the gap equally, in bus order.
#[test]
fn two_infinite_units_split_the_gap_equally() {
    let units = vec![
        Dispatchable {
            bus: 4,
            output: 0.0,
            p_min: f64::NEG_INFINITY,
            p_max: f64::INFINITY,
        },
        Dispatchable {
            bus: 1,
            output: 0.0,
            p_min: f64::NEG_INFINITY,
            p_max: f64::INFINITY,
        },
    ];

    let plan = rebalance(2.0, &units);

    assert_eq!(plan.changes.len(), 2);
    assert_eq!(plan.changes[0].0, 1);
    assert_eq!(plan.changes[1].0, 4);
    assert!((plan.changes[0].1 - 1.0).abs() < 1e-12);
    assert!((plan.changes[1].1 - 1.0).abs() < 1e-12);
    assert!((plan.residual - 0.0).abs() < 1e-12);
}
