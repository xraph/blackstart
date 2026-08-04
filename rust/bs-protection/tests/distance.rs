use bs_protection::{Distance, Zone};

/// Apparent impedance at V = 1.0 pu is 1/|P|, so a zone with reach Z trips
/// when |P| > 1/Z. Zone 1 reaches 0.8·x, so on a line of x = 0.1 the reach is
/// 0.08 and the threshold is 12.5 pu.
///
/// Isolated to zone 1 alone: under the full default zone set 12.4 pu already
/// clears zone 2's lower threshold (8.333 pu), so this checks zone 1's own
/// reach in isolation rather than the relay's combined behavior.
#[test]
fn zone_one_trips_above_its_reach_threshold() {
    let relay = Distance {
        zones: vec![Zone {
            reach: 0.8,
            delay: 0.0,
        }],
        breaker_time: 0.05,
    };

    assert_eq!(relay.time_to_trip(12.4, 0.1), None);

    let (t, zone) = relay.time_to_trip(12.6, 0.1).expect("inside zone 1");
    assert_eq!(zone, 1);
    assert!((t - relay.breaker_time).abs() < 1e-12, "got {t}");
}

/// The widest zone that sees the fault decides, and it is the fastest one that
/// wins — zone 1 is both narrower and quicker.
///
/// A flow inside zone 3 but outside zone 1 must report zone 3's delay. Picking
/// the first matching zone in list order rather than the fastest matching one
/// is the easy mistake, and it makes every zone-3 trip instantaneous.
#[test]
fn a_flow_only_zone_three_sees_takes_zone_threes_delay() {
    let relay = Distance::default();

    // reach 1.5·0.1 = 0.15 → threshold 6.667 pu; zone 2 is 8.333, zone 1 12.5.
    let (t, zone) = relay.time_to_trip(7.0, 0.1).expect("inside zone 3 only");

    assert_eq!(zone, 3);
    assert!((t - relay.breaker_time - 1.0).abs() < 1e-12, "got {t}");
}

/// Zone 1 is faster than zone 3 and must win when both see it.
#[test]
fn the_fastest_zone_that_sees_it_wins() {
    let relay = Distance::default();

    let (t, zone) = relay
        .time_to_trip(50.0, 0.1)
        .expect("deep inside every zone");

    assert_eq!(zone, 1);
    assert!((t - relay.breaker_time).abs() < 1e-12, "got {t}");
}

/// No flow is infinite apparent impedance, which no zone sees.
#[test]
fn zero_flow_trips_nothing() {
    assert_eq!(Distance::default().time_to_trip(0.0, 0.1), None);
}

/// A relay with no zones is a relay that is not there.
#[test]
fn an_empty_zone_list_never_trips() {
    let relay = Distance {
        zones: Vec::new(),
        breaker_time: 0.05,
    };

    assert_eq!(relay.time_to_trip(1e6, 0.1), None);
}

/// A degenerate reactance would divide by zero. `Case::validate` already
/// rejects it, but this crate is callable without a validated case.
#[test]
fn zero_reactance_trips_nothing() {
    assert_eq!(Distance::default().time_to_trip(10.0, 0.0), None);
}

#[test]
fn zones_are_configuration_not_constants() {
    let relay = Distance {
        zones: vec![Zone {
            reach: 2.0,
            delay: 0.4,
        }],
        breaker_time: 0.05,
    };

    // reach 2.0·0.1 = 0.2 → threshold 5.0
    let (t, zone) = relay
        .time_to_trip(6.0, 0.1)
        .expect("inside the single zone");
    assert_eq!(zone, 1);
    assert!((t - relay.breaker_time - 0.4).abs() < 1e-12, "got {t}");
}
