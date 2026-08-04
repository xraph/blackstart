use bs_frequency::{Aggregate, Defaults, Model, integrate};

fn model(inertia_h: f64, inverse_droop: f64) -> Model {
    Model {
        aggregate: Aggregate {
            inertia_h,
            inverse_droop,
            generation_base: 1.0,
            machines: 1,
        },
        defaults: Defaults::default(),
    }
}

/// A balanced island does not move.
#[test]
fn no_imbalance_is_no_deviation() {
    let got = integrate(&model(4.0, 20.0), 0.0, 30.0, 0.01);

    assert!(got.rocof_hz_s.abs() < 1e-12);
    assert!(got.nadir_hz.abs() < 1e-9, "nadir {}", got.nadir_hz);
    assert!(got.settling_hz.abs() < 1e-9);
}

/// `RoCoF` is the first thing the model asserts and the easiest to check:
///   df/dt = −ΔP·f₀/(2H) = −0.1·60/8 = −0.75 Hz/s
#[test]
fn initial_rocof_follows_the_swing_equation() {
    let got = integrate(&model(4.0, 20.0), 0.1, 30.0, 0.01);

    assert!(
        (got.rocof_hz_s + 0.75).abs() < 1e-9,
        "got {}",
        got.rocof_hz_s
    );
}

/// Settling frequency has a closed form the integration must land on:
///   `Δf_ss` = −ΔP/(D + 1/R) = −0.1/(1 + 20) = −0.004762 pu = −0.2857 Hz
#[test]
fn settling_matches_the_closed_form() {
    let m = model(4.0, 20.0);
    let got = integrate(&m, 0.1, 120.0, 0.01);

    let expected_hz = -0.1 / 21.0 * 60.0;

    assert!(
        (got.settling_hz - expected_hz).abs() < 1e-3,
        "got {}, want {expected_hz}",
        got.settling_hz
    );
    assert!((m.settling(0.1) * 60.0 - expected_hz).abs() < 1e-9);
}

/// The nadir undershoots the settling point and arrives before it.
///
/// A model that returns the settling value as its nadir has no dynamics in it
/// at all, and would report every island as surviving comfortably.
#[test]
fn the_nadir_undershoots_the_settling_point() {
    let got = integrate(&model(4.0, 20.0), 0.1, 120.0, 0.01);

    assert!(
        got.nadir_hz < got.settling_hz,
        "nadir {} should be below settling {}",
        got.nadir_hz,
        got.settling_hz
    );
    assert!(
        got.nadir_at > 0.0 && got.nadir_at < 120.0,
        "at {}",
        got.nadir_at
    );
}

/// More inertia means a slower fall and a shallower nadir. This is the whole
/// reason inertia is carried.
#[test]
fn more_inertia_means_a_shallower_nadir() {
    let light = integrate(&model(2.0, 20.0), 0.1, 120.0, 0.01);
    let heavy = integrate(&model(8.0, 20.0), 0.1, 120.0, 0.01);

    assert!(
        heavy.rocof_hz_s > light.rocof_hz_s,
        "heavier must fall slower"
    );
    assert!(heavy.nadir_hz > light.nadir_hz, "heavier must dip less far");
}

/// Surplus generation drives frequency up, with the same magnitudes mirrored.
#[test]
fn a_generation_surplus_raises_frequency() {
    let got = integrate(&model(4.0, 20.0), -0.1, 120.0, 0.01);

    assert!(got.rocof_hz_s > 0.0);
    assert!(got.settling_hz > 0.0);
}

/// Same input, same trajectory, every time. Integration is a floating-point
/// reduction and the whole determinism argument rests on it.
#[test]
fn integration_is_reproducible() {
    let first = integrate(&model(4.0, 20.0), 0.1, 30.0, 0.01);
    let second = integrate(&model(4.0, 20.0), 0.1, 30.0, 0.01);

    assert_eq!(first.samples, second.samples);
    assert_eq!(first.nadir_hz.to_bits(), second.nadir_hz.to_bits());
}

/// An island with no governor still finds a settling point, held up by load
/// damping alone: `Δf_ss` = −ΔP/D = −0.1/1 = −0.1 pu = −6 Hz.
#[test]
fn damping_alone_arrests_the_fall() {
    let got = integrate(&model(4.0, 0.0), 0.1, 300.0, 0.01);

    assert!(
        (got.settling_hz + 6.0).abs() < 1e-2,
        "got {}",
        got.settling_hz
    );
}
