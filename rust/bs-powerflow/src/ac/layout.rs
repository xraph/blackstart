//! Which unknowns the AC power flow solves for, and in what order.
//!
//! Each bus kind contributes different unknowns:
//!
//! | Kind  | Known            | Unknown          |
//! |-------|------------------|------------------|
//! | Slack | θ, \|V\|         | P, Q             |
//! | PV    | P, \|V\|         | θ, Q             |
//! | PQ    | P, Q             | θ, \|V\|         |
//!
//! So the state vector is every non-slack bus's angle followed by every PQ
//! bus's voltage magnitude, and the mismatch vector is the matching real-power
//! residuals followed by the reactive ones. The two must be ordered
//! identically or the Jacobian describes a different system than the one being
//! solved — which converges to something, just not the answer.
//!
//! Ordering is by ascending bus index within each block, which comes from case
//! order and is therefore canonical. Reordering for fill-in would change the
//! floating-point summation and is a decision for the factorizer, not for
//! here.

use bs_case::{BusKind, Case, CaseError};

/// The variable ordering for one case.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Layout {
    /// The reference bus, which contributes no unknowns.
    pub slack: usize,
    /// Buses with an angle unknown — every bus except the slack — ascending.
    pub angle_buses: Vec<usize>,
    /// Buses with a voltage-magnitude unknown — the PQ buses — ascending.
    pub magnitude_buses: Vec<usize>,
    /// Row of each bus in the angle block, or `usize::MAX` for the slack.
    angle_row: Vec<usize>,
    /// Row of each bus in the magnitude block, already offset past the angle
    /// block, or `usize::MAX` for a bus with no magnitude unknown.
    magnitude_row: Vec<usize>,
}

impl Layout {
    /// Builds the layout for a case.
    ///
    /// # Errors
    ///
    /// Returns [`CaseError`] when the slack bus cannot be identified.
    pub fn build(case: &Case) -> Result<Self, CaseError> {
        let slack = case.slack_index()?;
        let n = case.n_bus();

        let mut angle_buses = Vec::with_capacity(n.saturating_sub(1));
        let mut magnitude_buses = Vec::new();
        let mut angle_row = vec![usize::MAX; n];
        let mut magnitude_row = vec![usize::MAX; n];

        for (bus, spec) in case.buses.iter().enumerate() {
            if bus == slack {
                continue;
            }

            angle_row[bus] = angle_buses.len();
            angle_buses.push(bus);

            if spec.kind == BusKind::Pq {
                magnitude_buses.push(bus);
            }
        }

        // The magnitude block sits after the angle block in the same vector,
        // so its rows are offset once here rather than at every index site.
        for (i, &bus) in magnitude_buses.iter().enumerate() {
            magnitude_row[bus] = angle_buses.len() + i;
        }

        Ok(Self {
            slack,
            angle_buses,
            magnitude_buses,
            angle_row,
            magnitude_row,
        })
    }

    /// Total number of unknowns, and so the dimension of the Jacobian.
    #[must_use]
    pub fn len(&self) -> usize {
        self.angle_buses.len() + self.magnitude_buses.len()
    }

    /// Whether there is nothing to solve, which happens for a single-bus case.
    #[must_use]
    pub fn is_empty(&self) -> bool {
        self.len() == 0
    }

    /// Number of angle unknowns, which is also where the magnitude block
    /// starts.
    #[must_use]
    pub fn n_angle(&self) -> usize {
        self.angle_buses.len()
    }

    /// Row of this bus's angle unknown, or `None` for the slack.
    #[must_use]
    pub fn angle_row(&self, bus: usize) -> Option<usize> {
        match self.angle_row[bus] {
            usize::MAX => None,
            row => Some(row),
        }
    }

    /// Row of this bus's voltage-magnitude unknown, or `None` when it has
    /// none — the slack and every PV bus hold their magnitude fixed.
    #[must_use]
    pub fn magnitude_row(&self, bus: usize) -> Option<usize> {
        match self.magnitude_row[bus] {
            usize::MAX => None,
            row => Some(row),
        }
    }

    /// Applies a Newton step to a voltage state, in the layout's own order.
    ///
    /// Kept here rather than in the solver because this is the one place that
    /// knows what each row of the step vector means, and scattering that
    /// knowledge is how an update ends up applying a magnitude correction to
    /// an angle.
    ///
    /// # Panics
    ///
    /// Panics if `step` is not one entry per unknown.
    pub fn apply(&self, step: &[f64], angles: &mut [f64], magnitudes: &mut [f64]) {
        assert_eq!(step.len(), self.len(), "step must be one entry per unknown");

        for (row, &bus) in self.angle_buses.iter().enumerate() {
            angles[bus] += step[row];
        }

        for (i, &bus) in self.magnitude_buses.iter().enumerate() {
            magnitudes[bus] += step[self.angle_buses.len() + i];
        }
    }
}
