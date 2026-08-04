//! Under-frequency load shedding, and what becomes of an island.
//!
//! Stages are carried by the caller with NO default. An absent scheme means no
//! under-frequency shedding, and the result says so — following the reasoning
//! already recorded on `VoltageLimits` in bs-contingency: a shedding schedule
//! is a utility's policy, and a tool that supplies one by default is asserting
//! a policy it has no standing to assert.
//!
//! Protection settings get defaults because a relay set at its own line's
//! rating is a physical convention. A shedding schedule is not.

use crate::integrate::{Trajectory, integrate};
use crate::model::Model;

/// One shedding stage.
#[derive(Debug, Clone, Copy, PartialEq)]
pub struct Stage {
    /// Absolute frequency at which this stage picks up, hertz.
    pub at_hz: f64,
    /// Fraction of the island's remaining load it sheds.
    pub shed_fraction: f64,
    /// How long frequency must stay below the threshold, seconds.
    pub delay: f64,
}

/// A utility's shedding scheme, plus the survival band.
#[derive(Debug, Clone, PartialEq)]
pub struct Scheme {
    /// Stages, applied in the order given.
    pub stages: Vec<Stage>,
    /// Below this, with the scheme exhausted, the island has collapsed.
    pub floor_hz: f64,
    /// Above this, generation trips.
    pub ceiling_hz: f64,
}

impl Scheme {
    /// No shedding scheme at all.
    ///
    /// Named rather than derived from `Default`, so that a caller reaches for
    /// it deliberately and a reader sees that no scheme was supplied rather
    /// than that one was forgotten.
    #[must_use]
    pub fn none() -> Self {
        Self {
            stages: Vec::new(),
            floor_hz: 57.0,
            ceiling_hz: 61.8,
        }
    }
}

/// What became of an island.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum IslandCondition {
    /// Frequency stayed inside the band and no stage came close.
    Stable,
    /// The nadir went below the first stage's threshold but no shed was
    /// needed. The island came close and pulled back, which is a distinction
    /// an operator reviewing a cascade wants drawn.
    Recovering,
    /// At least one stage operated and frequency settled inside the band.
    Shedding,
    /// Frequency left the band with the scheme exhausted.
    Collapsed,
}

/// One stage operating.
#[derive(Debug, Clone, Copy, PartialEq)]
pub struct Shed {
    /// Seconds from the disturbance.
    pub at: f64,
    /// Fraction of the island's load shed by this stage.
    pub fraction: f64,
    /// Which stage, 1-based.
    pub stage: usize,
}

/// The island's whole frequency response.
#[derive(Debug, Clone, PartialEq)]
pub struct Response {
    /// What became of it.
    pub condition: IslandCondition,
    /// Stages that operated, in the order they did.
    pub sheds: Vec<Shed>,
    /// Initial rate of change, hertz per second.
    pub rocof_hz_s: f64,
    /// Lowest frequency reached, as a deviation in hertz.
    pub nadir_hz: f64,
    /// Where it settled, as a deviation in hertz.
    pub settling_hz: f64,
    /// Total fraction of the island's load shed.
    pub shed_fraction: f64,
}

/// The island's whole frequency response, shedding included.
///
/// `served_load` is the island's total load, per unit. It is a parameter
/// rather than something inferred from the imbalance because a stage sheds a
/// fraction OF THE LOAD: ten per cent of a 0.4 pu shortfall and ten per cent
/// of a 4.0 pu island are different actions, and only the caller knows which
/// is meant.
#[must_use]
pub fn respond(
    model: &Model,
    imbalance: f64,
    served_load: f64,
    scheme: &Scheme,
    horizon: f64,
    step: f64,
) -> Response {
    let nominal = model.defaults.nominal_hz;
    let mut remaining = imbalance;
    let mut load = served_load;
    let mut sheds = Vec::new();
    let mut elapsed = 0.0;
    let mut next_stage = 0;

    let mut trajectory = integrate(model, remaining, horizon, step);
    let rocof_hz_s = trajectory.rocof_hz_s;
    let mut nadir_hz = trajectory.nadir_hz;

    // Each stage gets at most one chance, in order, and every crossing
    // restarts the trajectory from rest with the reduced imbalance. Carrying
    // the state across instead would need a resumable integrator, which would
    // be a second source of truth about the island.
    while next_stage < scheme.stages.len() {
        let stage = scheme.stages[next_stage];

        let Some(at) = crossing(&trajectory, nominal, stage.at_hz, stage.delay) else {
            break;
        };

        let shed = stage.shed_fraction * load;

        sheds.push(Shed {
            at: elapsed + at,
            fraction: stage.shed_fraction,
            stage: next_stage + 1,
        });

        elapsed += at;
        load -= shed;
        remaining -= shed;
        next_stage += 1;

        trajectory = integrate(model, remaining, horizon, step);
        nadir_hz = nadir_hz.min(trajectory.nadir_hz);
    }

    let settling_hz = trajectory.settling_hz;
    let shed_fraction = if served_load > 0.0 {
        (served_load - load) / served_load
    } else {
        0.0
    };

    Response {
        condition: classify(scheme, nominal, nadir_hz, settling_hz, !sheds.is_empty()),
        sheds,
        rocof_hz_s,
        nadir_hz,
        settling_hz,
        shed_fraction,
    }
}

/// When the trajectory first sits below `threshold` for `delay` seconds.
///
/// The delay is a real requirement, not a formality: a transient dip that
/// recovers before the timer expires is the difference between an island that
/// sheds and one that does not, and it is exactly the `Recovering` case.
fn crossing(trajectory: &Trajectory, nominal: f64, threshold: f64, delay: f64) -> Option<f64> {
    for (index, &(time, deviation)) in trajectory.samples.iter().enumerate() {
        if nominal + deviation >= threshold {
            continue;
        }

        // Below the threshold. It has to stay below for the whole delay.
        let held = trajectory.samples[index..]
            .iter()
            .take_while(|(t, _)| *t <= time + delay)
            .all(|(_, d)| nominal + d < threshold);

        let reached_the_end = trajectory
            .samples
            .last()
            .is_some_and(|(t, _)| *t >= time + delay);

        if held && reached_the_end {
            return Some(time + delay);
        }
    }

    None
}

/// What became of the island, given where it went and where it settled.
fn classify(
    scheme: &Scheme,
    nominal: f64,
    nadir_hz: f64,
    settling_hz: f64,
    shed: bool,
) -> IslandCondition {
    if nominal + settling_hz < scheme.floor_hz || nominal + settling_hz > scheme.ceiling_hz {
        return IslandCondition::Collapsed;
    }

    if shed {
        return IslandCondition::Shedding;
    }

    // Recovering is the distinction worth drawing: the nadir went below the
    // first stage's threshold but no shed was needed. Reporting that as stable
    // would hide an island that came within a hair of shedding.
    let first = scheme.stages.first().map_or(f64::NEG_INFINITY, |s| s.at_hz);

    if nominal + nadir_hz < first {
        IslandCondition::Recovering
    } else {
        IslandCondition::Stable
    }
}
