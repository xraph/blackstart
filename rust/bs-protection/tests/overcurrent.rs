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
/// Same rule as `assess` in bs-contingency, which skips a branch whose rating
/// is not positive rather than dividing by it. So the consequence of a zero
/// rating is that the branch drops out of every violation check: the element
/// reports no trip, the screen reports no overload, and a line at 180% of its
/// true limit reads as an all-clear. A zero suppresses a violation, it does
/// not invent one, which is why a rating that could not be derived must be
/// absent rather than zero.
#[test]
fn an_unrated_branch_has_no_pickup() {
    let relay = Overcurrent::default();

    assert_eq!(relay.time_to_trip(500.0, 0.0), None);
}

/// Direction does not matter to an overcurrent element.
///
/// 1.5 pu on a 1.0 rating is chosen deliberately: it sits above pickup
/// (1.0×) but below the default instantaneous multiple (2.5×), so both
/// signs land on the inverse-time CURVE rather than the flat instantaneous
/// element. At 3.0 pu — this test's previous value — both signs hit the
/// instantaneous element instead and return the same flat `breaker_time`
/// constant, which a missing `.abs()` would also happen to pass on the
/// positive side and fail only by returning `None` on the negative side; it
/// never exercised the curve's own arithmetic on a negative input.
#[test]
fn pickup_is_on_magnitude() {
    let relay = Overcurrent::default();

    let positive = relay
        .time_to_trip(1.5, 1.0)
        .expect("above pickup, below instantaneous, trips");
    let negative = relay
        .time_to_trip(-1.5, 1.0)
        .expect("above pickup, below instantaneous, trips");

    assert!(
        (positive - negative).abs() < 1e-12,
        "got {positive} vs {negative}"
    );

    // Off the flat breaker floor, so this is actually the curve's own
    // number, not a coincidental match with the instantaneous path.
    assert!(
        positive > relay.breaker_time,
        "got {positive}, which looks like the flat floor, not a curve value"
    );
}
