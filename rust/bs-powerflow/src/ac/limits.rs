//! Voltage assessment for AC solutions.
//!
//! Assessing an AC solution against a voltage band is the power-flow crate's
//! domain. These types live here rather than in the contingency crate so that
//! `bs-cascade` (and other downstream users) can import them without depending
//! on contingency analysis.

/// Voltage limits applied during verification, per unit.
///
/// Defaults are the usual transmission planning band. They are configuration
/// rather than constants because the acceptable band is a utility's decision,
/// and a tool that hard-codes one is asserting a policy it has no standing to
/// assert.
#[derive(Debug, Clone, Copy, PartialEq)]
pub struct VoltageLimits {
    /// Lower limit.
    pub min: f64,
    /// Upper limit.
    pub max: f64,
}

impl Default for VoltageLimits {
    fn default() -> Self {
        Self {
            min: 0.95,
            max: 1.05,
        }
    }
}

/// A bus outside its voltage limits.
#[derive(Debug, Clone, PartialEq)]
pub struct VoltageViolation {
    /// Index of the bus.
    pub bus: usize,
    /// The magnitude it settled at, per unit.
    pub magnitude: f64,
    /// The limit it crossed.
    pub limit: f64,
}
