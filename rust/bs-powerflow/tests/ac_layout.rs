//! Which unknowns the AC power flow solves for, and in what order.
//!
//! The exact comparisons here are deliberate: the slack's angle and a PV bus's
//! magnitude must be left *untouched* by a Newton step, so equality is what is
//! being asserted and a tolerance would let a small wrong update through — the
//! kind that does not fail, it just converges somewhere else.
#![allow(clippy::float_cmp)]

use bs_case::{Branch, Bus, Case};
use bs_powerflow::ac::Layout;
use bs_quantity::Base;

fn base() -> Base {
    Base::new(100.0, 138.0).unwrap()
}

fn close(got: f64, want: f64, tol: f64) -> bool {
    (got - want).abs() <= tol * want.abs().max(1.0)
}

/// Slack at 0, PQ at 1 and 2, PV at 3.
fn mixed() -> Case {
    Case::new(
        "mixed",
        base(),
        vec![
            Bus::slack("s", 1.0),
            Bus::load("a", -0.4, -0.1),
            Bus::load("b", -0.7, -0.2),
            Bus::generator("g", 0.3, 1.02),
        ],
        vec![
            Branch::new("s-a", 0, 1, 0.02, 0.10),
            Branch::new("a-b", 1, 2, 0.03, 0.15),
            Branch::new("b-g", 2, 3, 0.02, 0.12),
            Branch::new("g-s", 3, 0, 0.01, 0.08),
        ],
    )
}

#[test]
fn each_bus_kind_contributes_the_unknowns_it_should() {
    let layout = Layout::build(&mixed()).unwrap();

    // Every bus but the slack has an angle unknown.
    assert_eq!(layout.angle_buses, vec![1, 2, 3]);
    // Only PQ buses have a magnitude unknown: the PV bus holds its scheduled
    // voltage and the slack holds both.
    assert_eq!(layout.magnitude_buses, vec![1, 2]);

    assert_eq!(layout.len(), 5);
    assert_eq!(layout.n_angle(), 3);

    assert_eq!(layout.angle_row(0), None, "the slack has no angle unknown");
    assert_eq!(layout.angle_row(1), Some(0));
    assert_eq!(layout.angle_row(3), Some(2));

    assert_eq!(layout.magnitude_row(0), None);
    assert_eq!(
        layout.magnitude_row(3),
        None,
        "a PV bus holds its magnitude"
    );
    // The magnitude block sits after the angle block in one vector.
    assert_eq!(layout.magnitude_row(1), Some(3));
    assert_eq!(layout.magnitude_row(2), Some(4));
}

/// A step must land where the layout says it does. Getting this wrong applies
/// a magnitude correction to an angle, which does not fail — it converges to
/// something else.
#[test]
fn a_step_updates_exactly_the_unknowns_it_names() {
    let case = mixed();
    let layout = Layout::build(&case).unwrap();

    // Plain vectors rather than a Voltages: apply takes slices, and a test
    // that reaches for the richer type would be asserting the coupling it is
    // supposed to be free of.
    let mut angle = vec![0.0; case.n_bus()];
    let mut magnitude: Vec<f64> = case.buses.iter().map(|b| b.v_magnitude).collect();
    let before = magnitude.clone();

    // One distinguishable value per unknown.
    layout.apply(&[0.1, 0.2, 0.3, 0.01, 0.02], &mut angle, &mut magnitude);

    assert!(close(angle[1], 0.1, 1e-15));
    assert!(close(angle[2], 0.2, 1e-15));
    assert!(close(angle[3], 0.3, 1e-15));
    assert_eq!(angle[0], 0.0, "the slack angle moved");

    assert!(close(magnitude[1], before[1] + 0.01, 1e-15));
    assert!(close(magnitude[2], before[2] + 0.02, 1e-15));
    assert_eq!(magnitude[3], before[3], "a PV bus's magnitude moved");
    assert_eq!(magnitude[0], before[0], "the slack magnitude moved");
}
