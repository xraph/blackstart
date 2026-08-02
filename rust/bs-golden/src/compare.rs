//! Comparing a solution against an oracle's.
//!
//! The output is designed around one rule: on failure, print the worst
//! offending element, never a two-thousand-line diff. A report nobody reads is
//! the same as no report, and a 300-bus disagreement dumped in full is a
//! report nobody reads.

use bs_powerflow::ac::Voltages;

use crate::document::Reference;

/// Per-quantity agreement thresholds.
///
/// These are chosen by measurement against the committed goldens and the
/// reasoning is recorded in [`Tolerances::default`]. A tolerance picked to
/// make a test pass is not a tolerance — it is a way of not knowing — so each
/// one here has a stated margin against what is actually observed.
#[derive(Debug, Clone, Copy, PartialEq)]
pub struct Tolerances {
    /// Absolute agreement on voltage magnitude, per unit.
    pub magnitude: f64,
    /// Absolute agreement on voltage angle, radians.
    pub angle: f64,
}

impl Default for Tolerances {
    fn default() -> Self {
        // Both solvers converge to a bus-mismatch residual near 1e-10, so the
        // states themselves agree far more tightly than this. 1e-6 leaves
        // roughly three orders of magnitude of headroom: enough that a
        // different BLAS or a different rounding order never trips it, tight
        // enough that a genuine modelling difference — a dropped shunt, a tap
        // applied at the wrong end — cannot hide underneath. Those show up at
        // 1e-3 and larger.
        //
        // Angle is looser than magnitude only because an angle near zero has
        // no meaningful relative scale; the absolute figure is what matters.
        Self {
            magnitude: 1e-6,
            angle: 1e-6,
        }
    }
}

/// One element where two solutions disagree.
#[derive(Debug, Clone, PartialEq)]
pub struct Disagreement {
    /// Which quantity.
    pub quantity: &'static str,
    /// Which bus.
    pub bus: usize,
    /// What this solver said.
    pub ours: f64,
    /// What the oracle said.
    pub theirs: f64,
    /// Absolute difference.
    pub error: f64,
    /// The threshold it exceeded.
    pub tolerance: f64,
}

impl core::fmt::Display for Disagreement {
    fn fmt(&self, f: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        write!(
            f,
            "{} at bus {}: {} vs {} (error {:.3e}, tolerance {:.3e})",
            self.quantity, self.bus, self.ours, self.theirs, self.error, self.tolerance
        )
    }
}

/// The result of comparing against an oracle.
#[derive(Debug, Clone, PartialEq)]
pub struct Comparison {
    /// Every element outside tolerance, worst first.
    pub disagreements: Vec<Disagreement>,
    /// The largest magnitude error seen, in agreement or not.
    pub worst_magnitude: f64,
    /// The largest angle error seen.
    pub worst_angle: f64,
}

impl Comparison {
    /// Whether the two solutions agree everywhere.
    #[must_use]
    pub fn agrees(&self) -> bool {
        self.disagreements.is_empty()
    }

    /// A short report naming the worst offenders.
    ///
    /// Capped deliberately. The whole point of ranking is that the first few
    /// lines say what is wrong; printing all two thousand says nothing and
    /// buries the ones that matter.
    #[must_use]
    pub fn report(&self, limit: usize) -> String {
        if self.agrees() {
            return format!(
                "agrees (worst magnitude {:.3e}, worst angle {:.3e})",
                self.worst_magnitude, self.worst_angle
            );
        }

        let mut out = format!("{} elements disagree; worst:\n", self.disagreements.len());

        for disagreement in self.disagreements.iter().take(limit) {
            out.push_str("  ");
            out.push_str(&disagreement.to_string());
            out.push('\n');
        }

        if self.disagreements.len() > limit {
            out.push_str(&format!(
                "  … and {} more\n",
                self.disagreements.len() - limit
            ));
        }

        out
    }
}

/// Compares a solved voltage state against an oracle's.
///
/// # Panics
///
/// Panics if the two do not cover the same number of buses. That is a loading
/// problem, not a numerical one, and comparing anyway would line up unrelated
/// buses and report nonsense.
#[must_use]
pub fn compare(ours: &Voltages, reference: &Reference, tolerances: Tolerances) -> Comparison {
    assert_eq!(
        ours.magnitude.len(),
        reference.v_magnitude.len(),
        "solutions cover different numbers of buses"
    );

    let mut disagreements = Vec::new();
    let mut worst_magnitude = 0.0_f64;
    let mut worst_angle = 0.0_f64;

    for bus in 0..ours.magnitude.len() {
        let magnitude_error = (ours.magnitude[bus] - reference.v_magnitude[bus]).abs();
        worst_magnitude = worst_magnitude.max(magnitude_error);

        if magnitude_error > tolerances.magnitude {
            disagreements.push(Disagreement {
                quantity: "|V|",
                bus,
                ours: ours.magnitude[bus],
                theirs: reference.v_magnitude[bus],
                error: magnitude_error,
                tolerance: tolerances.magnitude,
            });
        }

        // Angles are compared after wrapping the difference into (−π, π], so
        // two states that differ by a full turn are not reported as a
        // catastrophic disagreement over an irrelevant convention.
        let angle_error = wrap(ours.angle[bus] - reference.v_angle[bus]).abs();
        worst_angle = worst_angle.max(angle_error);

        if angle_error > tolerances.angle {
            disagreements.push(Disagreement {
                quantity: "θ",
                bus,
                ours: ours.angle[bus],
                theirs: reference.v_angle[bus],
                error: angle_error,
                tolerance: tolerances.angle,
            });
        }
    }

    disagreements.sort_by(|a, b| {
        b.error
            .partial_cmp(&a.error)
            .unwrap_or(core::cmp::Ordering::Equal)
            .then(a.bus.cmp(&b.bus))
    });

    Comparison {
        disagreements,
        worst_magnitude,
        worst_angle,
    }
}

/// Wraps an angle difference into (−π, π].
fn wrap(radians: f64) -> f64 {
    let two_pi = core::f64::consts::TAU;
    let wrapped = radians % two_pi;

    if wrapped > core::f64::consts::PI {
        wrapped - two_pi
    } else if wrapped <= -core::f64::consts::PI {
        wrapped + two_pi
    } else {
        wrapped
    }
}
