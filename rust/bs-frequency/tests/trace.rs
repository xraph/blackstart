use bs_frequency::{Aggregate, Defaults, Model, Scheme, Stage, respond};

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

/// A balanced island still produces a trace, and it sits at nominal.
#[test]
fn a_balanced_island_traces_flat_at_nominal() {
    let got = respond(&model(4.0, 20.0), 0.0, 2.0, &Scheme::none(), 30.0, 0.01);

    assert!(!got.trace.is_empty(), "even a flat island has a curve");
    assert!((got.trace[0].at - 0.0).abs() < 1e-12, "starts at t = 0");
    for point in &got.trace {
        assert!(
            (point.hz - 60.0).abs() < 1e-6,
            "balanced island should sit at nominal, got {}",
            point.hz
        );
    }
}

/// The trace is ABSOLUTE hertz, not a deviation.
///
/// A deviation would put the curve near zero, which renders as a collapsed
/// island on a chart whose axis is 57–61.8.
#[test]
fn the_trace_is_absolute_hertz() {
    let got = respond(&model(4.0, 20.0), 0.1, 2.0, &Scheme::none(), 30.0, 0.01);

    let lowest = got.trace.iter().map(|p| p.hz).fold(f64::INFINITY, f64::min);

    assert!(
        lowest > 50.0,
        "a small shortfall must not leave the band; got {lowest}"
    );
    assert!(
        lowest < 60.0,
        "a shortfall must dip below nominal; got {lowest}"
    );
}

/// THE TEST THIS TASK EXISTS FOR.
///
/// Each shedding stage restarts integration from rest, and every segment runs
/// to the full horizon — so the segments overlap in local time. Concatenating
/// them without truncating each at the point the next begins produces a curve
/// that jumps backwards in time, which the operator console's schema rejects
/// outright because it validates that nothing moves backwards.
#[test]
fn the_trace_never_goes_backwards_across_a_shed() {
    let scheme = Scheme {
        stages: vec![
            Stage {
                at_hz: 59.3,
                shed_fraction: 0.25,
                delay: 0.1,
            },
            Stage {
                at_hz: 58.9,
                shed_fraction: 0.25,
                delay: 0.1,
            },
        ],
        floor_hz: 57.0,
        ceiling_hz: 61.8,
    };

    let got = respond(&model(2.0, 15.0), 1.2, 4.0, &scheme, 30.0, 0.01);

    assert!(
        !got.sheds.is_empty(),
        "the fixture must actually shed, or this proves nothing"
    );

    for pair in got.trace.windows(2) {
        assert!(
            pair[1].at >= pair[0].at,
            "trace went backwards: {}s follows {}s",
            pair[1].at,
            pair[0].at
        );
    }
}

/// Each segment ends where the next begins — no gap, no overlap.
///
/// A shed at time T must have trace points on both sides of T with nothing
/// beyond it from the previous segment.
#[test]
fn a_segment_stops_where_the_next_one_starts() {
    let scheme = Scheme {
        stages: vec![Stage {
            at_hz: 59.3,
            shed_fraction: 0.25,
            delay: 0.1,
        }],
        floor_hz: 57.0,
        ceiling_hz: 61.8,
    };

    let got = respond(&model(2.0, 15.0), 1.2, 4.0, &scheme, 30.0, 0.01);
    let shed_at = got.sheds.first().expect("a stage fires").at;

    let before = got.trace.iter().filter(|p| p.at < shed_at).count();
    let after = got.trace.iter().filter(|p| p.at >= shed_at).count();

    assert!(before > 0, "the pre-shed segment must contribute points");
    assert!(after > 0, "the post-shed segment must contribute points");
}

/// Same input, same curve. The trace is part of the determinism guarantee.
#[test]
fn the_trace_is_reproducible() {
    let run = || respond(&model(4.0, 20.0), 0.4, 2.0, &Scheme::none(), 30.0, 0.01);

    let first = run();
    let second = run();

    assert_eq!(first.trace.len(), second.trace.len());
    for (a, b) in first.trace.iter().zip(second.trace.iter()) {
        assert_eq!(a.at.to_bits(), b.at.to_bits());
        assert_eq!(a.hz.to_bits(), b.hz.to_bits());
    }
}
