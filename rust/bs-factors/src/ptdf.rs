//! Power transfer distribution factors.
//!
//! `PTDF[l][k]` is the change in flow on branch `l` per unit injected at bus
//! `k` and withdrawn at the slack. Because the DC model is linear, this
//! depends only on the network — not on the operating point — so it is built
//! once and reused.

use bs_case::{Case, CaseError};
use bs_powerflow::{ReduceError, Reduction};
use bs_sparse::{DenseLu, Error as SparseError, Factorize};

/// Why sensitivity factors could not be built.
#[derive(Debug, Clone, PartialEq)]
pub enum FactorError {
    /// The case itself is not solvable.
    Case(CaseError),
    /// The reduced system could not be factorized, which after
    /// `Case::validate` has passed almost always means the network is
    /// disconnected and an island has no slack bus.
    Linear(SparseError),
    /// The outage of this branch would disconnect the network, so its flow has
    /// nowhere to redistribute to and no distribution factor exists.
    ///
    /// This is not a failure to compute — it is a real property of the
    /// network, and a radial branch's outage has to be simulated rather than
    /// screened.
    Radial { branch: usize },
    /// The branch is already out of service, so it has no flow to redistribute.
    OutOfService { branch: usize },
}

impl core::fmt::Display for FactorError {
    fn fmt(&self, f: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        match self {
            Self::Case(e) => write!(f, "case: {e}"),
            Self::Linear(e) => write!(
                f,
                "linear solve: {e}. The case validated, so this usually means \
                 the network is disconnected and an island has no slack bus."
            ),
            Self::Radial { branch } => write!(
                f,
                "branch {branch} is radial: its outage disconnects the network, \
                 so its flow has nowhere to redistribute and no distribution \
                 factor exists. Simulate this contingency rather than screening it."
            ),
            Self::OutOfService { branch } => write!(
                f,
                "branch {branch} is already out of service and carries no flow \
                 to redistribute"
            ),
        }
    }
}

impl std::error::Error for FactorError {}

impl From<CaseError> for FactorError {
    fn from(e: CaseError) -> Self {
        Self::Case(e)
    }
}

impl From<SparseError> for FactorError {
    fn from(e: SparseError) -> Self {
        Self::Linear(e)
    }
}

impl From<ReduceError> for FactorError {
    fn from(e: ReduceError) -> Self {
        match e {
            ReduceError::Case(c) => Self::Case(c),
            ReduceError::Sparse(s) => Self::Linear(s),
        }
    }
}

/// Power transfer distribution factors, `n_branch` by `n_bus`.
#[derive(Debug, Clone, PartialEq)]
pub struct Ptdf {
    n_branch: usize,
    n_bus: usize,
    slack: usize,
    /// Row-major: `values[branch * n_bus + bus]`.
    values: Vec<f64>,
}

impl Ptdf {
    /// Builds the factors for a case.
    ///
    /// One factorization, then one back-substitution per non-slack bus. The
    /// slack column is left at zero rather than computed: injecting at the
    /// slack and withdrawing at the slack is the null transfer, and its column
    /// is zero by definition rather than by arithmetic that might not quite
    /// land there.
    ///
    /// Buses are walked in reduced order, which comes from case order, so the
    /// solve sequence is identical every run.
    ///
    /// # Errors
    ///
    /// Returns [`FactorError`] when the case is invalid or the reduced system
    /// is singular.
    pub fn build(case: &Case) -> Result<Self, FactorError> {
        case.validate()?;

        let reduction = Reduction::build(case)?;
        let (n_branch, n_bus) = (case.n_branch(), case.n_bus());

        let mut ptdf = Self {
            n_branch,
            n_bus,
            slack: reduction.slack,
            values: vec![0.0; n_branch * n_bus],
        };

        // A single-bus network has no transfers to distribute.
        if reduction.is_empty() {
            return Ok(ptdf);
        }

        let lu = DenseLu;
        let factors = lu.factor(&reduction.matrix)?;

        let mut unit = vec![0.0; reduction.len()];

        for (reduced, &bus) in reduction.full_of.iter().enumerate() {
            // A unit injection at this bus, withdrawn at the slack. The
            // withdrawal is implicit: the slack row was deleted, so whatever
            // the rest of the network does not absorb goes there.
            unit[reduced] = 1.0;
            let angles = reduction.expand(&lu.solve(&factors, &unit)?);
            unit[reduced] = 0.0;

            for (branch, flow) in bs_powerflow::branch_flows(case, &angles)
                .into_iter()
                .enumerate()
            {
                ptdf.values[branch * n_bus + bus] = flow;
            }
        }

        Ok(ptdf)
    }

    /// Number of branches, in service or not.
    #[must_use]
    pub fn n_branch(&self) -> usize {
        self.n_branch
    }

    /// Number of buses.
    #[must_use]
    pub fn n_bus(&self) -> usize {
        self.n_bus
    }

    /// Index of the slack bus, whose column is zero.
    #[must_use]
    pub fn slack(&self) -> usize {
        self.slack
    }

    /// Sensitivity of branch `l` to an injection at bus `k` withdrawn at the
    /// slack.
    ///
    /// # Panics
    ///
    /// Panics if either index is out of range. These are matrix indices on a
    /// hot path; returning an Option would push the check to every caller and
    /// most would unwrap it anyway.
    #[must_use]
    pub fn get(&self, branch: usize, bus: usize) -> f64 {
        assert!(branch < self.n_branch, "branch {branch} out of range");
        assert!(bus < self.n_bus, "bus {bus} out of range");

        self.values[branch * self.n_bus + bus]
    }

    /// Sensitivity of every branch to a transfer of one unit from `from` to
    /// `to`, in case branch order.
    ///
    /// This is the difference of two columns. Because both are measured
    /// against the same slack, the slack cancels — which is why a transfer
    /// factor is independent of which bus was chosen as the reference, even
    /// though the columns it is built from are not.
    ///
    /// # Panics
    ///
    /// Panics if either bus index is out of range.
    #[must_use]
    pub fn transfer(&self, from: usize, to: usize) -> Vec<f64> {
        (0..self.n_branch)
            .map(|branch| self.get(branch, from) - self.get(branch, to))
            .collect()
    }

    /// Branch flows produced by an injection vector, in case branch order.
    ///
    /// Exact for the DC model: this is the same answer `solve_dc` gives, and a
    /// test asserts it, because a factor that disagrees with the solve it was
    /// derived from is worse than no factor at all.
    ///
    /// # Panics
    ///
    /// Panics if `injections` is not one entry per bus.
    #[must_use]
    pub fn flows(&self, injections: &[f64]) -> Vec<f64> {
        assert_eq!(
            injections.len(),
            self.n_bus,
            "injections must have one entry per bus"
        );

        (0..self.n_branch)
            .map(|branch| {
                // Summed in bus order rather than by any faster arrangement:
                // the order of a floating-point reduction is part of the
                // answer, and this one has to match the solve.
                (0..self.n_bus)
                    .map(|bus| self.get(branch, bus) * injections[bus])
                    .sum()
            })
            .collect()
    }
}
