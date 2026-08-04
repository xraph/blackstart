use bs_frequency::{Aggregate, Defaults, IslandCondition, Model, Scheme, Stage, respond};

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

/// Every test below passes a served load of 2.0 per unit, so a 10% stage
/// sheds 0.2 pu. Stated once here rather than justified at each call site.
fn three_stage() -> Scheme {
    Scheme {
        stages: vec![
            Stage {
                at_hz: 59.3,
                shed_fraction: 0.10,
                delay: 0.1,
            },
            Stage {
                at_hz: 58.9,
                shed_fraction: 0.10,
                delay: 0.1,
            },
            Stage {
                at_hz: 58.5,
                shed_fraction: 0.15,
                delay: 0.1,
            },
        ],
        floor_hz: 57.0,
        ceiling_hz: 61.8,
    }
}

/// A balanced island is stable and sheds nothing.
#[test]
fn a_balanced_island_is_stable() {
    let got = respond(&model(4.0, 20.0), 0.0, 2.0, &three_stage(), 60.0, 0.01);

    assert_eq!(got.condition, IslandCondition::Stable);
    assert!(got.sheds.is_empty());
    assert!((got.shed_fraction - 0.0).abs() < 1e-12);
}

/// A small shortfall the governor covers without reaching the first stage.
#[test]
fn a_small_shortfall_settles_without_shedding() {
    let got = respond(&model(4.0, 20.0), 0.05, 2.0, &three_stage(), 60.0, 0.01);

    assert_eq!(got.condition, IslandCondition::Stable);
    assert!(got.sheds.is_empty());
    assert!(got.settling_hz > -0.7, "settling {}", got.settling_hz);
}

/// A shortfall deep enough to cross the first stage sheds, and the shed shows
/// up both as an event and in the total.
#[test]
fn crossing_the_first_stage_sheds_load() {
    let got = respond(&model(4.0, 20.0), 0.4, 2.0, &three_stage(), 60.0, 0.01);

    assert_eq!(got.condition, IslandCondition::Shedding);
    assert!(!got.sheds.is_empty(), "expected at least one stage to fire");
    assert_eq!(got.sheds[0].stage, 1);
    assert!(got.sheds[0].at > 0.0);
    assert!(got.shed_fraction >= 0.10);
}

/// With no scheme, nothing sheds — whatever the frequency does.
///
/// An absent scheme means no under-frequency shedding, not a default one. A
/// tool that supplied a shedding schedule by default would be asserting a
/// utility's policy it has no standing to assert.
#[test]
fn an_absent_scheme_sheds_nothing() {
    let got = respond(&model(4.0, 20.0), 0.8, 2.0, &Scheme::none(), 60.0, 0.01);

    assert!(got.sheds.is_empty());
    assert!((got.shed_fraction - 0.0).abs() < 1e-12);
}

/// Frequency below the floor with the scheme exhausted is a collapse, and a
/// collapse is reported as one rather than as a deep but surviving island.
#[test]
fn an_island_below_the_floor_collapses() {
    let got = respond(&model(1.0, 0.0), 5.0, 2.0, &Scheme::none(), 60.0, 0.01);

    assert_eq!(got.condition, IslandCondition::Collapsed);
}

/// Recovering is the distinction worth drawing: the nadir went below the first
/// stage's threshold but no shed was needed to bring it back.
///
/// Reporting this as `stable` would hide an island that came within a hair of
/// shedding, which is exactly what an operator reviewing a cascade wants to
/// see.
#[test]
fn an_island_that_dips_and_returns_is_recovering() {
    // Light inertia gives a sharp, brief nadir; a strong governor (inverse
    // droop 60) pulls the deviation back above the threshold well inside the
    // stage's 5 s delay. Measured directly against the trajectory: with
    // these constants the deviation sits below -0.7 Hz (59.3 Hz) from
    // t=0.15s to t=1.38s, a 1.23 s excursion, so the 5 s timer never expires
    // and no stage fires.
    let scheme = Scheme {
        stages: vec![Stage {
            at_hz: 59.3,
            shed_fraction: 0.10,
            delay: 5.0,
        }],
        floor_hz: 57.0,
        ceiling_hz: 61.8,
    };

    let got = respond(&model(1.0, 60.0), 0.3, 2.0, &scheme, 60.0, 0.01);

    assert_eq!(got.condition, IslandCondition::Recovering);
    assert!(got.sheds.is_empty());
    assert!(
        got.nadir_hz < -0.7,
        "nadir {} should cross 59.3",
        got.nadir_hz
    );
}

/// A surplus drives frequency up and trips generation rather than shedding.
#[test]
fn a_surplus_above_the_ceiling_is_not_a_shed() {
    let got = respond(&model(4.0, 20.0), -1.0, 2.0, &three_stage(), 60.0, 0.01);

    assert!(got.sheds.is_empty(), "over-frequency does not shed load");
    assert!(got.settling_hz > 0.0);
}

/// Same input, same answer.
#[test]
fn the_response_is_reproducible() {
    let first = respond(&model(4.0, 20.0), 0.4, 2.0, &three_stage(), 60.0, 0.01);
    let second = respond(&model(4.0, 20.0), 0.4, 2.0, &three_stage(), 60.0, 0.01);

    assert_eq!(first.sheds, second.sheds);
    assert_eq!(first.nadir_hz.to_bits(), second.nadir_hz.to_bits());
}

/// THE TEST THIS TASK EXISTS FOR.
///
/// `solver.proto` documents a guarantee on `Response.nadir_hz`'s wire
/// counterpart: the decimation downstream keeps each bucket's extrema
/// rather than sampling at a stride, "so the nadir reported ... is
/// guaranteed to appear on the curve too." That guarantee is only as good
/// as what `respond` hands the wire in the first place — and derived from
/// the raw trajectories rather than the assembled `trace`, it could be
/// false: every non-final trajectory keeps integrating past the crossing
/// that supersedes it, all the way to the full horizon, and that
/// counterfactual tail is exactly what `push_segment` truncates away when
/// it assembles `trace`. A multi-stage shed is what exercises this: with
/// two stages firing, TWO trajectories (everything but the last) carry a
/// tail the island never actually lived through.
///
/// This model and scheme are shared with
/// `trace.rs::the_trace_never_goes_backwards_across_a_shed`, which already
/// establishes that this fixture sheds — reused rather than re-derived so a
/// reader checking one test's fixture against the other does not have to
/// wonder whether they were meant to agree.
#[test]
fn nadir_hz_is_the_minimum_of_the_trace() {
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

    assert_eq!(
        got.sheds.len(),
        2,
        "the fixture must actually fire both stages, or this proves nothing \
         about a MULTI-stage shed: {:?}",
        got.sheds
    );

    let nominal = Defaults::default().nominal_hz;
    let trace_min_hz = got
        .trace
        .iter()
        .map(|sample| sample.hz)
        .fold(f64::INFINITY, f64::min);

    // Bit-for-bit, not an epsilon comparison: `nadir_hz` and `trace`'s own
    // minimum are computed from the very same samples, so a correct
    // implementation reproduces the identical `f64`, not merely a nearby
    // one — the same standard `the_response_is_reproducible` above already
    // holds `respond` to.
    assert_eq!(
        got.nadir_hz.to_bits(),
        (trace_min_hz - nominal).to_bits(),
        "nadir_hz ({}) must equal the trace's own minimum, {} Hz absolute \
         (a deviation of {}) — a curve that disagreed with the number \
         printed beside it would be worse than a coarse one",
        got.nadir_hz,
        trace_min_hz,
        trace_min_hz - nominal
    );
}
