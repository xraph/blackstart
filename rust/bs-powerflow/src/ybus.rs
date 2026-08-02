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
    /// `y = 1 / (r + jx)` to both diagonals and, negated, to both off-diagonal
    /// positions, plus half its total charging susceptance to each diagonal.
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

            g.add(from, from, series_g)?;
            g.add(to, to, series_g)?;
            g.add(from, to, -series_g)?;
            g.add(to, from, -series_g)?;

            b.add(from, from, series_b + half_charging)?;
            b.add(to, to, series_b + half_charging)?;
            b.add(from, to, -series_b)?;
            b.add(to, from, -series_b)?;
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
#[derive(Debug, Clone, PartialEq, Eq)]
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
