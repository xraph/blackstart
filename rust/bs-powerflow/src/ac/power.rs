//! Bus power injections and the mismatch they leave.
//!
//! The two equations everything else in the AC solver derives from:
//!
//! ```text
//! P_i = |V_i| Σ_j |V_j| (G_ij cos θ_ij + B_ij sin θ_ij)
//! Q_i = |V_i| Σ_j |V_j| (G_ij sin θ_ij − B_ij cos θ_ij)
//! ```
//!
//! where `θ_ij = θ_i − θ_j`. The sums run over the sparsity pattern rather
//! than every bus, which is the whole reason `Ybus` is sparse.

use crate::ac::layout::Layout;
use crate::ybus::Ybus;

/// A voltage state: magnitudes and angles, indexed as the case is.
#[derive(Debug, Clone, PartialEq)]
pub struct Voltages {
    /// Magnitude in per unit.
    pub magnitude: Vec<f64>,
    /// Angle in radians.
    pub angle: Vec<f64>,
}

impl Voltages {
    /// A flat start: every magnitude at its scheduled value, every angle at
    /// the reference.
    ///
    /// Flat is the standard starting point and is deliberate rather than lazy.
    /// It is the state a well-behaved transmission network is near, so Newton
    /// converges from it in a handful of iterations; starting somewhere
    /// "cleverer" makes convergence depend on the previous answer, which makes
    /// a result depend on what was solved before it.
    ///
    /// "Flat" means every angle equal to the slack's, not every angle zero.
    /// Those coincide only when the reference is scheduled at zero, and a case
    /// whose reference is elsewhere — IEEE 118 puts it at 30° — would
    /// otherwise be solved against the wrong reference. Every angle then comes
    /// out shifted by a constant, which is easy to miss because every *flow*
    /// is still correct: flows depend on angle differences alone.
    #[must_use]
    pub fn flat(case: &bs_case::Case) -> Self {
        let reference = case
            .slack_index()
            .map_or(0.0, |slack| case.buses[slack].v_angle);

        Self {
            magnitude: case.buses.iter().map(|b| b.v_magnitude).collect(),
            angle: vec![reference; case.n_bus()],
        }
    }

    /// Number of buses.
    #[must_use]
    pub fn len(&self) -> usize {
        self.magnitude.len()
    }

    /// Whether the state is empty.
    #[must_use]
    pub fn is_empty(&self) -> bool {
        self.magnitude.is_empty()
    }
}

/// Real and reactive power injected at every bus by the network.
#[derive(Debug, Clone, PartialEq)]
pub struct Injections {
    /// Real power, per unit.
    pub p: Vec<f64>,
    /// Reactive power, per unit.
    pub q: Vec<f64>,
}

/// Computes the power injected at every bus for a voltage state.
///
/// Both matrices share one sparsity pattern, so the two rows are walked
/// together and each `(column, value)` pair is used once. Walking them
/// separately would double the trigonometry, which dominates this loop.
///
/// # Panics
///
/// Panics if the voltage state does not have one entry per bus.
#[must_use]
pub fn injections(ybus: &Ybus, volts: &Voltages) -> Injections {
    assert_eq!(
        volts.len(),
        ybus.n_bus(),
        "voltage state must cover every bus"
    );

    let n = ybus.n_bus();
    let mut p = vec![0.0; n];
    let mut q = vec![0.0; n];

    for bus in 0..n {
        let (mut p_sum, mut q_sum) = (0.0, 0.0);

        // Summed in ascending column order, which is what Csr::row yields and
        // what makes the reduction reproducible.
        for ((col, conductance), (col_b, susceptance)) in ybus.g.row(bus).zip(ybus.b.row(bus)) {
            debug_assert_eq!(col, col_b, "G and B must share a sparsity pattern");

            let (sin, cos) = (volts.angle[bus] - volts.angle[col]).sin_cos();
            let neighbour = volts.magnitude[col];

            p_sum += neighbour * conductance.mul_add(cos, susceptance * sin);
            q_sum += neighbour * conductance.mul_add(sin, -(susceptance * cos));
        }

        p[bus] = volts.magnitude[bus] * p_sum;
        q[bus] = volts.magnitude[bus] * q_sum;
    }

    Injections { p, q }
}

/// The mismatch between scheduled and computed power, in the layout's order.
///
/// Real-power residuals for every non-slack bus, then reactive residuals for
/// every PQ bus — matching the state vector exactly, because the Jacobian maps
/// one onto the other and a disagreement would describe a different system
/// than the one being solved.
///
/// The slack's residual is not included: its injection is whatever the network
/// requires, so it has no equation to satisfy. Neither is a PV bus's reactive
/// residual, for the same reason — its reactive output is free until a limit
/// binds it, which is what `qlimit` handles.
///
/// # Panics
///
/// Panics if the injections do not cover every bus.
#[must_use]
pub fn mismatch(case: &bs_case::Case, layout: &Layout, computed: &Injections) -> Vec<f64> {
    assert_eq!(
        computed.p.len(),
        case.n_bus(),
        "injections must cover every bus"
    );

    let mut out = Vec::with_capacity(layout.len());

    for &bus in &layout.angle_buses {
        out.push(case.buses[bus].p_injection - computed.p[bus]);
    }

    for &bus in &layout.magnitude_buses {
        out.push(case.buses[bus].q_injection - computed.q[bus]);
    }

    out
}

/// The largest absolute mismatch, which is what convergence is judged on.
///
/// The infinity norm rather than a Euclidean one: convergence has to mean
/// "every bus balances", and a two-norm lets one badly unbalanced bus hide
/// behind a thousand converged ones as the network grows.
#[must_use]
pub fn max_mismatch(mismatch: &[f64]) -> f64 {
    mismatch.iter().fold(0.0_f64, |worst, m| worst.max(m.abs()))
}
