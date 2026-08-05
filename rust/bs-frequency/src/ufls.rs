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

/// One point on an island's frequency curve.
///
/// Absolute hertz, not a deviation. A deviation is what the integrator works
/// in, but it is the wrong thing to hand a display: a curve near zero renders
/// as a collapsed island on an axis calibrated 57 to 61.8.
#[derive(Debug, Clone, Copy, PartialEq)]
pub struct Sample {
    /// Seconds from the start of this response's disturbance.
    pub at: f64,
    /// Absolute frequency, hertz.
    pub hz: f64,
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
    /// The frequency curve, absolute hertz, strictly non-decreasing in time.
    ///
    /// Assembled rather than simply concatenated. Every shedding stage
    /// restarts integration from rest and each segment runs to the full
    /// horizon, so the segments overlap in local time — appending them raw
    /// would produce a curve that jumps backwards. Each is truncated where
    /// the next begins. See `push_segment`.
    pub trace: Vec<Sample>,
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
    let mut trace: Vec<Sample> = Vec::new();
    let mut elapsed = 0.0;
    let mut next_stage = 0;

    let mut trajectory = integrate(model, remaining, horizon, step);
    let rocof_hz_s = trajectory.rocof_hz_s;

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

        // Emitted before `elapsed` advances: this segment occupies the
        // response clock from the CURRENT elapsed up to the crossing, and the
        // next integration takes over from there.
        push_segment(&mut trace, &trajectory, nominal, elapsed, Some(at));

        elapsed += at;
        load -= shed;
        remaining -= shed;
        next_stage += 1;

        trajectory = integrate(model, remaining, horizon, step);
    }

    push_segment(&mut trace, &trajectory, nominal, elapsed, None);

    // The nadir reported here must be a point the assembled trace actually
    // passes through — that is the guarantee `solver.proto` documents on the
    // wire type's own `nadir_hz`. Pulling it from the raw trajectories (as
    // this used to, via `Trajectory::nadir_hz`) reports the minimum over
    // EVERY segment's full integration horizon, including the counterfactual
    // tail past a shed crossing — the part `push_segment` truncates away
    // below because the island was superseded there and never actually
    // reached it. `trace` is exactly that truncated, actual path, so its
    // minimum is the true nadir by construction, and it is already
    // full-resolution (decimation happens later, in `bs-cascade`), so this is
    // exact rather than an approximation.
    //
    // Sign convention: `trace` holds absolute hertz (see `Sample`), while
    // `Response::nadir_hz` is a deviation (see below and `classify`, which
    // adds `nominal` back on to compare against absolute stage thresholds) —
    // so the nominal offset comes back out here, once, at the point of
    // derivation.
    //
    // An island with no machine never reaches `respond` at all — `step.rs`
    // skips straight to `None` for it — and every trajectory this function
    // does integrate seeds `samples` with `(0.0, 0.0)` (see `integrate`), so
    // `trace` is never actually empty here. The `is_finite` fallback exists
    // only so a violation of that invariant reports "no dip observed" (`0.0`,
    // the same convention `Trajectory::nadir_hz` uses for an all-flat
    // trajectory) instead of the `f64::INFINITY` an empty fold would
    // otherwise produce.
    let min_hz = trace
        .iter()
        .fold(f64::INFINITY, |min, sample| min.min(sample.hz));
    let nadir_hz = if min_hz.is_finite() {
        min_hz - nominal
    } else {
        0.0
    };

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
        trace,
    }
}

/// Appends one integration segment to the assembled trace.
///
/// `offset` is where this segment begins on the response's own clock, and
/// `until` bounds it: `Some(t)` when a later segment takes over at local time
/// `t`, `None` for the final segment, which runs to the horizon.
///
/// The bound is what makes the assembled trace monotonic. Without it every
/// segment contributes its whole horizon and the curve doubles back on itself
/// at each shed.
fn push_segment(
    into: &mut Vec<Sample>,
    trajectory: &Trajectory,
    nominal: f64,
    offset: f64,
    until: Option<f64>,
) {
    for &(at, deviation) in &trajectory.samples {
        if let Some(limit) = until
            && at >= limit
        {
            break;
        }

        into.push(Sample {
            at: offset + at,
            hz: nominal + deviation,
        });
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
///
/// Public so `bs-cascade` can re-derive a step's own condition from the
/// samples that step actually contributed — `bound`'s own truncation means
/// `Response::condition` (this same function, called once here over the
/// FULL, unbounded trajectory) is not always the verdict a caller stepping
/// through the run one bound slice at a time should fold in. See
/// `bs-cascade::simulate::accumulate_bus_history` for where and why.
#[must_use]
pub fn classify(
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
