//! The reduced system-frequency-response model.
//!
//! Two states: frequency deviation and the reheat lag. One equivalent machine
//! with a reheat turbine, which is the standard reduction for exactly this
//! question — how far does frequency fall and how fast — without carrying a
//! rotor angle per generator.
//!
//! Sign convention, which the whole model has to agree on: `imbalance` is
//! positive for a generation SHORTFALL. A positive imbalance therefore drives
//! the deviation negative and the governor term positive.

use crate::machine::{Aggregate, Defaults};

/// The model's state, both terms per unit.
#[derive(Debug, Clone, Copy, PartialEq, Default)]
pub struct State {
    /// Frequency deviation from nominal, per unit.
    pub deviation: f64,
    /// The reheat lag state, per unit.
    pub reheat: f64,
}

/// One island's frequency model.
#[derive(Debug, Clone, Copy, PartialEq)]
pub struct Model {
    /// The island reduced to one machine.
    pub aggregate: Aggregate,
    /// Characteristics the machines do not state.
    pub defaults: Defaults,
}

impl Model {
    /// Mechanical power the governor produces at this state, per unit.
    #[must_use]
    pub fn mechanical(&self, state: State) -> f64 {
        let f_h = self.defaults.high_pressure_fraction;

        -self.aggregate.inverse_droop * (f_h * state.deviation + (1.0 - f_h) * state.reheat)
    }

    /// The state derivative.
    #[must_use]
    pub fn derivative(&self, state: State, imbalance: f64) -> State {
        let mechanical = self.mechanical(state);
        let damping = self.defaults.damping * state.deviation;

        State {
            deviation: (mechanical - imbalance - damping) / (2.0 * self.aggregate.inertia_h),
            reheat: (state.deviation - state.reheat) / self.defaults.reheat_time,
        }
    }

    /// The deviation this island settles at, per unit.
    ///
    /// At rest the reheat state equals the deviation, so the whole governor
    /// response collapses to `−Δf/R` and the balance gives
    /// `Δf = −ΔP/(D + 1/R)`. Provided as a closed form because it is what the
    /// integration must converge to, which makes it a test rather than a
    /// second implementation.
    #[must_use]
    pub fn settling(&self, imbalance: f64) -> f64 {
        let stiffness = self.defaults.damping + self.aggregate.inverse_droop;

        if stiffness <= 0.0 {
            // No damping and no governor: nothing arrests the fall, and there
            // is no settling point to report.
            return f64::NAN;
        }

        -imbalance / stiffness
    }
}
