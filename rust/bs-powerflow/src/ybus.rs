//! The bus admittance matrix.
//!
//! `Y = G + jB` relates bus voltages to injected currents. It is stored as two
//! real matrices rather than one complex one because the polar Newton-Raphson
//! formulation uses `G` and `B` separately in every term:
//!
//! ```text
//! P_i = V_i Σ_j V_j (G_ij cos θ_ij + B_ij sin θ_ij)
//! Q_i = V_i Σ_j V_j (G_ij sin θ_ij − B_ij cos θ_ij)
//! ```
//!
//! so a complex type would be built and immediately taken apart. Keeping them
//! split also means no complex-number dependency and no complex arithmetic on
//! the inner loop, where every operation is counted.
//!
//! Both matrices share one sparsity pattern — the network's — so they are
//! assembled together and indexed identically.

use bs_case::{Case, CaseError};
use bs_sparse::{Csr, Error as SparseError, Triplets};

/// The admittance matrix, split into conductance and susceptance.
#[derive(Debug, Clone)]
pub struct Ybus {
    /// Real part: conductance.
    pub g: Csr,
    /// Imaginary part: susceptance.
    pub b: Csr,
}

impl Ybus {
    /// Assembles the admittance matrix for a case.
    ///
    /// Each in-service branch contributes its series admittance
    /// `y = 1 / (r + jx)` and half its charging susceptance, transformed by
    /// the off-nominal turns ratio and phase shift. With `τ = tap · e^{j·shift}`
    /// applied at the from-end, the four entries are
    ///
    /// ```text
    /// Y_ff = (y + jb/2) / |τ|²      Y_ft = −y / conj(τ)
    /// Y_tf = −y / τ                 Y_tt =  y + jb/2
    /// ```
    ///
    /// which is the MATPOWER and pandapower convention, so an imported case
    /// needs no reinterpretation. For a line — `tap` 1, `shift` 0 — this
    /// reduces to the symmetric form.
    ///
    /// A phase shift makes the matrix asymmetric: `Y_ft ≠ Y_tf`. That is
    /// physical, not a bug, and is why the symmetry test only covers networks
    /// without shifters.
    ///
    /// Bus shunts land on the diagonal.
    ///
    /// Branches are walked in case order, which is canonical, so the triplet
    /// sequence — and therefore the summation order once compressed — is
    /// identical every run.
    ///
    /// # Errors
    ///
    /// Returns [`CaseError`] if the case is structurally invalid, or
    /// [`SparseError`] if assembly overflows the declared dimensions.
    pub fn build(case: &Case) -> Result<Self, YbusError> {
        case.validate()?;

        let n = case.n_bus();

        let mut g = Triplets::new(n, n);
        let mut b = Triplets::new(n, n);
        g.reserve(case.n_branch() * 4 + n);
        b.reserve(case.n_branch() * 4 + n);

        for branch in &case.branches {
            if !branch.in_service {
                continue;
            }

            // y = 1 / (r + jx) = (r − jx) / (r² + x²). Written out rather than
            // via a complex type: this is the only complex division in the
            // crate, and Case::validate has already ruled out x = 0, so the
            // denominator cannot be zero.
            let denom = branch.r.mul_add(branch.r, branch.x * branch.x);
            let (series_g, series_b) = (branch.r / denom, -branch.x / denom);

            let (from, to) = (branch.from, branch.to);

            // Line charging is a shunt to ground at each end, so half goes to
            // each terminal's diagonal and none to the off-diagonals.
            let half_charging = branch.b / 2.0;

            // The to-end sees the branch as it is; the from-end sees it
            // through the transformer, scaled by |τ|².
            let tap_squared = branch.tap * branch.tap;

            g.add(to, to, series_g)?;
            b.add(to, to, series_b + half_charging)?;

            g.add(from, from, series_g / tap_squared)?;
            b.add(from, from, (series_b + half_charging) / tap_squared)?;

            // Off-diagonals divide by the complex ratio, which for a phase
            // shifter differs between the two directions — the from→to term
            // uses conj(τ) and the to→from term uses τ, so a shifter makes Y
            // legitimately asymmetric.
            let (sin_shift, cos_shift) = branch.shift.sin_cos();

            // −y / conj(τ) with τ = tap·e^{j·shift}.
            let ft_g = -(series_g * cos_shift - series_b * sin_shift) / branch.tap;
            let ft_b = -(series_b * cos_shift + series_g * sin_shift) / branch.tap;

            // −y / τ: the same with the shift negated.
            let tf_g = -(series_g * cos_shift + series_b * sin_shift) / branch.tap;
            let tf_b = -(series_b * cos_shift - series_g * sin_shift) / branch.tap;

            g.add(from, to, ft_g)?;
            b.add(from, to, ft_b)?;
            g.add(to, from, tf_g)?;
            b.add(to, from, tf_b)?;
        }

        // Bus shunts — capacitor banks and reactors — go straight onto the
        // diagonal. Emitted after the branches so that a bus with a shunt but
        // no branch still gets an entry rather than an empty row.
        for (bus, spec) in case.buses.iter().enumerate() {
            if spec.g_shunt != 0.0 {
                g.add(bus, bus, spec.g_shunt)?;
            }
            if spec.b_shunt != 0.0 {
                b.add(bus, bus, spec.b_shunt)?;
            }
        }

        Ok(Self {
            g: g.compress(),
            b: b.compress(),
        })
    }

    /// Number of buses.
    #[must_use]
    pub fn n_bus(&self) -> usize {
        self.g.rows()
    }
}

/// Why an admittance matrix could not be assembled.
#[derive(Debug, Clone, PartialEq)]
pub enum YbusError {
    /// The case itself is invalid.
    Case(CaseError),
    /// Assembly exceeded the declared matrix dimensions.
    Sparse(SparseError),
}

impl core::fmt::Display for YbusError {
    fn fmt(&self, f: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        match self {
            Self::Case(e) => write!(f, "case: {e}"),
            Self::Sparse(e) => write!(f, "assembly: {e}"),
        }
    }
}

impl std::error::Error for YbusError {}

impl From<CaseError> for YbusError {
    fn from(e: CaseError) -> Self {
        Self::Case(e)
    }
}

impl From<SparseError> for YbusError {
    fn from(e: SparseError) -> Self {
        Self::Sparse(e)
    }
}
