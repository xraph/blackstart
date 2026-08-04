//! Fixed-step integration of the island frequency model.
//!
//! RK4 at a fixed step and a fixed order, deliberately. A variable-step
//! integrator would make the number of floating-point operations depend on the
//! trajectory taken, and therefore make the last bits of the answer depend on
//! it too — which is the determinism property this project treats as
//! correctness rather than tidiness.

use crate::model::{Model, State};

/// What one integration found.
#[derive(Debug, Clone, PartialEq)]
pub struct Trajectory {
    /// Initial rate of change, hertz per second. Negative for a shortfall.
    pub rocof_hz_s: f64,
    /// Lowest frequency deviation reached, hertz. Negative below nominal.
    pub nadir_hz: f64,
    /// When the nadir occurred, seconds.
    pub nadir_at: f64,
    /// Deviation at the end of the horizon, hertz.
    pub settling_hz: f64,
    /// `(time, deviation in hertz)` at every step, for the event log and for
    /// shedding to search.
    pub samples: Vec<(f64, f64)>,
}

/// Integrates the model over a horizon at a fixed step.
///
/// # Panics
///
/// Panics if `step` is not positive — an integration that never advances would
/// otherwise hang rather than report anything.
#[must_use]
pub fn integrate(model: &Model, imbalance: f64, horizon: f64, step: f64) -> Trajectory {
    assert!(step > 0.0, "integration step must be positive");

    let nominal = model.defaults.nominal_hz;
    let mut state = State::default();
    let mut time = 0.0;

    let rocof_hz_s = model.derivative(state, imbalance).deviation * nominal;

    // The step count is not known up front without the very cast this project
    // forbids (`(horizon / step).ceil() as usize`), which would also make a
    // reallocation-vs-not decision depend on a lossy float-to-int conversion.
    // A plain `Vec::new()` growing as samples arrive costs a handful of
    // reallocations over a run and keeps the loop bound entirely in `f64`.
    let mut samples = Vec::new();
    samples.push((0.0, 0.0));

    let mut nadir_hz = 0.0;
    let mut nadir_at = 0.0;

    // Accumulate time by repeated addition of `step` rather than deriving it
    // from an iteration count, so the sample times — and therefore the whole
    // trajectory — are reproduced bit-for-bit on every run with the same
    // inputs, per `integration_is_reproducible`.
    while time < horizon {
        state = advance(model, state, imbalance, step);
        time += step;

        let hz = state.deviation * nominal;
        samples.push((time, hz));

        if hz < nadir_hz {
            nadir_hz = hz;
            nadir_at = time;
        }
    }

    Trajectory {
        rocof_hz_s,
        nadir_hz,
        nadir_at,
        settling_hz: state.deviation * nominal,
        samples,
    }
}

/// One RK4 step.
fn advance(model: &Model, state: State, imbalance: f64, step: f64) -> State {
    let k1 = model.derivative(state, imbalance);
    let k2 = model.derivative(offset(state, k1, step / 2.0), imbalance);
    let k3 = model.derivative(offset(state, k2, step / 2.0), imbalance);
    let k4 = model.derivative(offset(state, k3, step), imbalance);

    State {
        deviation: state.deviation
            + step / 6.0 * (k1.deviation + 2.0 * k2.deviation + 2.0 * k3.deviation + k4.deviation),
        reheat: state.reheat
            + step / 6.0 * (k1.reheat + 2.0 * k2.reheat + 2.0 * k3.reheat + k4.reheat),
    }
}

fn offset(state: State, derivative: State, by: f64) -> State {
    State {
        deviation: state.deviation + derivative.deviation * by,
        reheat: state.reheat + derivative.reheat * by,
    }
}
