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
