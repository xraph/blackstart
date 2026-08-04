//! Overcurrent protection, as an inverse-time curve and an instantaneous
//! element.
//!
//! In the DC model every voltage is 1.0 per unit, so current magnitude and
//! real-power magnitude are the same number. That equivalence is why this
//! module takes a flow and never mentions current again — writing it in amps
//! would imply a precision the linear model does not have.

/// An IEC 60255-151 inverse-time characteristic.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub enum Curve {
    /// `A = 0.14, B = 0.02`. The usual transmission choice.
    #[default]
    StandardInverse,
    /// `A = 13.5, B = 1`.
    VeryInverse,
    /// `A = 80, B = 2`. Grades against fuses.
    ExtremelyInverse,
}

impl Curve {
    /// The `(A, B)` constants of the characteristic.
    #[must_use]
    pub fn constants(self) -> (f64, f64) {
        match self {
            Self::StandardInverse => (0.14, 0.02),
            Self::VeryInverse => (13.5, 1.0),
            Self::ExtremelyInverse => (80.0, 2.0),
        }
    }
}

/// An overcurrent element guarding one branch.
#[derive(Debug, Clone, Copy, PartialEq)]
pub struct Overcurrent {
    /// Pickup as a multiple of the branch rating.
    pub pickup: f64,
    /// Time multiplier setting, scaling the whole curve.
    pub time_multiplier: f64,
    /// Instantaneous element, as a multiple of the rating. `None` disables it.
    pub instantaneous: Option<f64>,
    /// Which characteristic.
    pub curve: Curve,
    /// Breaker operating time, seconds. The floor on any trip.
    pub breaker_time: f64,
}

impl Default for Overcurrent {
    fn default() -> Self {
        Self {
            pickup: 1.0,
            time_multiplier: 0.1,
            instantaneous: Some(2.5),
            curve: Curve::StandardInverse,
            breaker_time: 0.05,
        }
    }
}

impl Overcurrent {
    /// When this element trips, in seconds, or `None` if it does not.
    ///
    /// `None` rather than an enormous finite time below pickup: the curve
    /// diverges as the multiple approaches one, and a very large number would
    /// let a long enough simulation trip an element whose protection never
    /// operated.
    #[must_use]
    pub fn time_to_trip(&self, flow: f64, rating: f64) -> Option<f64> {
        // Unrated means the limit is unknown, not zero.
        if rating <= 0.0 || self.pickup <= 0.0 {
            return None;
        }

        let magnitude = flow.abs();

        if let Some(multiple) = self.instantaneous
            && magnitude >= multiple * rating
        {
            return Some(self.breaker_time);
        }

        let m = magnitude / (self.pickup * rating);

        if m <= 1.0 {
            return None;
        }

        let (a, b) = self.curve.constants();
        let t = self.time_multiplier * a / (m.powf(b) - 1.0);

        // The curve can return less than the breaker can physically achieve.
        Some(t.max(self.breaker_time))
    }
}
