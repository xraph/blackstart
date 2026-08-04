use bs_powerflow::ac::{VoltageLimits, VoltageViolation};

#[test]
fn the_default_band_is_the_usual_planning_one() {
    let limits = VoltageLimits::default();

    assert!((limits.min - 0.95).abs() < 1e-12);
    assert!((limits.max - 1.05).abs() < 1e-12);
}

#[test]
fn a_violation_names_the_limit_it_crossed() {
    let violation = VoltageViolation {
        bus: 3,
        magnitude: 0.91,
        limit: 0.95,
    };

    assert_eq!(violation.bus, 3);
    assert!((violation.limit - 0.95).abs() < 1e-12);
}
