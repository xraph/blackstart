//! Bus and branch construction.
//!
//! These assertions compare fields a constructor set verbatim against the
//! literal it was given — no arithmetic happens in between, so exact equality
//! is the correct assertion and an epsilon would only hide a builder that
//! silently rewrote its input.
#![allow(clippy::float_cmp)]

use bs_case::{Branch, Bus, BusKind};

#[test]
fn constructors_set_the_kind_they_name() {
    assert_eq!(Bus::load("a", -1.0, 0.0).kind, BusKind::Pq);
    assert_eq!(Bus::generator("b", 1.0, 1.02).kind, BusKind::Pv);
    assert_eq!(Bus::slack("c", 1.0).kind, BusKind::Slack);

    // A generator holds its scheduled voltage; a load starts flat.
    assert_eq!(Bus::generator("b", 1.0, 1.02).v_magnitude, 1.02);
    assert_eq!(Bus::load("a", -1.0, 0.0).v_magnitude, 1.0);
}

#[test]
fn builders_do_not_disturb_the_fields_they_do_not_set() {
    let branch = Branch::new("b", 0, 1, 0.02, 0.3)
        .with_rating(1.5)
        .with_charging(0.06);

    assert_eq!(branch.r, 0.02);
    assert_eq!(branch.x, 0.3);
    assert_eq!(branch.rating, 1.5);
    assert_eq!(branch.b, 0.06);
    assert!(branch.in_service);

    // Unrated is zero, not a sentinel that could be mistaken for a real limit.
    assert_eq!(Branch::new("c", 0, 1, 0.0, 0.1).rating, 0.0);
}

#[test]
fn a_generator_has_no_real_limits_until_given_them() {
    let bus = Bus::generator("g1", 1.0, 1.02);

    assert_eq!(bus.p_min, f64::NEG_INFINITY);
    assert_eq!(bus.p_max, f64::INFINITY);
}

#[test]
fn machine_data_is_absent_rather_than_zero() {
    // Zero inertia is a physical claim — an island with no rotating mass
    // collapses instantly. Unpopulated import data must not make it.
    let bus = Bus::generator("g1", 1.0, 1.02);

    assert_eq!(bus.inertia_h, None);
    assert_eq!(bus.droop_r, None);
}

#[test]
fn real_limits_and_machine_data_are_carried() {
    let bus = Bus::generator("g1", 1.0, 1.02)
        .with_real_limits(0.2, 1.5)
        .with_machine(4.5, 0.05);

    assert_eq!(bus.p_min, 0.2);
    assert_eq!(bus.p_max, 1.5);
    assert_eq!(bus.inertia_h, Some(4.5));
    assert_eq!(bus.droop_r, Some(0.05));
}
