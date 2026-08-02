//! The slack-reduced susceptance matrix, and the index maps that go with it.
//!
//! Extracted from the solve because the sensitivity factors in `bs-factors`
//! need the same matrix. Two assemblies of "the same" B would be two
//! opportunities to disagree — a different branch ordering alone changes the
//! floating-point summation and therefore the last bits of every answer — and
//! the whole point of a screen is that it agrees with the solve it approximates.

use bs_case::{Case, CaseError};
use bs_sparse::{Csr, Error as SparseError, Triplets};

/// A case reduced to its non-slack buses.
#[derive(Debug, Clone)]
pub struct Reduction {
    /// Index of the slack bus in the full case.
    pub slack: usize,
    /// Number of buses in the full case.
    pub n_bus: usize,
    /// Reduced index for each full bus index, or `usize::MAX` for the slack.
    pub reduced_of: Vec<usize>,
    /// Full bus index for each reduced index.
    pub full_of: Vec<usize>,
    /// The reduced susceptance matrix, `1/x` summed onto the diagonal and
    /// negated off it.
    pub matrix: Csr,
}

impl Reduction {
    /// Builds the reduction for a case.
    ///
    /// The case is assumed already validated — [`Case::validate`] is the place
    /// structural mistakes are named, and repeating it here would report the
    /// same problem twice in two different vocabularies.
    ///
    /// # Errors
    ///
    /// Returns [`CaseError`] if the slack bus cannot be identified, or
    /// [`SparseError`] if assembly overflows the declared dimensions.
    pub fn build(case: &Case) -> Result<Self, ReduceError> {
        let slack = case.slack_index()?;
        let n_bus = case.n_bus();

        // A Vec rather than a map: the lookup is on the inner assembly loop,
        // and the ordering has to be deterministic anyway.
        let mut reduced_of = vec![usize::MAX; n_bus];
        let mut full_of = Vec::with_capacity(n_bus.saturating_sub(1));

        for (bus, slot) in reduced_of.iter_mut().enumerate() {
            if bus == slack {
                continue;
            }
            *slot = full_of.len();
            full_of.push(bus);
        }

        let reduced_n = full_of.len();

        // Branches are walked in case order, which is canonical, so the triplet
        // sequence — and therefore the summation order once compressed — is
        // identical every run.
        let mut susceptances = Triplets::new(reduced_n, reduced_n);
        susceptances.reserve(case.n_branch() * 4);

        for branch in &case.branches {
            if !branch.in_service {
                continue;
            }

            let susceptance = 1.0 / branch.x;
            let (from, to) = (reduced_of[branch.from], reduced_of[branch.to]);

            // Diagonal terms accumulate from every incident branch;
            // off-diagonals are negative. Rows and columns belonging to the
            // slack are simply never emitted, which is what deleting them
            // means here.
            if from != usize::MAX {
                susceptances.add(from, from, susceptance)?;
            }
            if to != usize::MAX {
                susceptances.add(to, to, susceptance)?;
            }
            if from != usize::MAX && to != usize::MAX {
                susceptances.add(from, to, -susceptance)?;
                susceptances.add(to, from, -susceptance)?;
            }
        }

        Ok(Self {
            slack,
            n_bus,
            reduced_of,
            full_of,
            matrix: susceptances.compress(),
        })
    }

    /// Number of buses in the reduced system.
    #[must_use]
    pub fn len(&self) -> usize {
        self.full_of.len()
    }

    /// Whether the reduced system is empty, which happens for a single-bus
    /// case: the slack supplies everything and there is nothing to solve.
    #[must_use]
    pub fn is_empty(&self) -> bool {
        self.full_of.is_empty()
    }

    /// Expands a reduced vector to full bus indexing, with zero at the slack.
    #[must_use]
    pub fn expand(&self, reduced: &[f64]) -> Vec<f64> {
        let mut full = vec![0.0; self.n_bus];

        for (i, &bus) in self.full_of.iter().enumerate() {
            full[bus] = reduced[i];
        }

        full
    }
}

/// Why a case could not be reduced.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ReduceError {
    /// The case has no usable slack bus.
    Case(CaseError),
    /// Assembly exceeded the declared matrix dimensions.
    Sparse(SparseError),
}

impl core::fmt::Display for ReduceError {
    fn fmt(&self, f: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        match self {
            Self::Case(e) => write!(f, "case: {e}"),
            Self::Sparse(e) => write!(f, "assembly: {e}"),
        }
    }
}

impl std::error::Error for ReduceError {}

impl From<CaseError> for ReduceError {
    fn from(e: CaseError) -> Self {
        Self::Case(e)
    }
}

impl From<SparseError> for ReduceError {
    fn from(e: SparseError) -> Self {
        Self::Sparse(e)
    }
}
