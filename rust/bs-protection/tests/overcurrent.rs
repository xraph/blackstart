use bs_protection::{Curve, Overcurrent};

/// Below pickup there is no trip at all, not a very slow one.
///
/// An inverse-time curve tends to infinity as the multiple approaches one, so
/// a naive implementation returns an enormous but finite time and the element
/// trips at the end of a long enough simulation. That is a different physical
/// claim from "protection did not operate".
#[test]
fn below_pickup_never_trips() {
    let relay = Overcurrent::default();

    assert_eq!(relay.time_to_trip(0.9, 1.0), None);
    assert_eq!(relay.time_to_trip(1.0, 1.0), None);
}

/// IEC 60255-151 standard inverse at twice pickup, TMS 1.0:
///   t = 1.0 · 0.14 / (2^0.02 − 1) = 0.14 / `0.013_959`… = 10.0289…
#[test]
fn standard_inverse_matches_the_iec_curve() {
    let relay = Overcurrent {
        pickup: 1.0,
        time_multiplier: 1.0,
        instantaneous: None,
        curve: Curve::StandardInverse,
        breaker_time: 0.05,
    };

    let t = relay
        .time_to_trip(2.0, 1.0)
        .expect("two times pickup trips");

    assert!((t - 10.028_8).abs() < 1e-3, "got {t}");
}

/// The instantaneous element short-circuits the curve.
#[test]
fn instantaneous_overrides_the_curve() {
    let relay = Overcurrent {
        instantaneous: Some(2.5),
        ..Overcurrent::default()
    };

    let t = relay.time_to_trip(3.0, 1.0).expect("above instantaneous");

    assert!((t - relay.breaker_time).abs() < 1e-12, "got {t}");
}

/// An unrated branch has an unknown limit, not a limit of zero.
///
/// Same rule as `assess` in bs-contingency: dividing by a zero rating would
/// make every unrated branch infinitely overloaded and trip the whole network.
#[test]
fn an_unrated_branch_has_no_pickup() {
    let relay = Overcurrent::default();

    assert_eq!(relay.time_to_trip(500.0, 0.0), None);
}

/// Direction does not matter to an overcurrent element.
#[test]
fn pickup_is_on_magnitude() {
    let relay = Overcurrent::default();

    assert_eq!(relay.time_to_trip(-3.0, 1.0), relay.time_to_trip(3.0, 1.0));
}
