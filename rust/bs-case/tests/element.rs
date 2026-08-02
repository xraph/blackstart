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
