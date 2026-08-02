//! The DC power flow.

use std::fmt;

use bs_case::{Case, CaseError};
use bs_sparse::{DenseLu, Error as SparseError, Factorize};

use crate::reduce::{ReduceError, Reduction};

/// Why a power flow could not be solved.
#[derive(Debug, Clone, PartialEq)]
pub enum PowerFlowError {
    /// The case itself is not solvable.
    Case(CaseError),
    /// The linear system could not be factorized.
    ///
    /// After [`Case::validate`] has passed, this almost always means the
    /// network is disconnected: an island with no slack bus has no angle
    /// reference, so its block of the matrix is singular.
    Linear(SparseError),
}

impl fmt::Display for PowerFlowError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::Case(e) => write!(f, "case: {e}"),
            Self::Linear(e) => write!(
                f,
                "linear solve: {e}. The case validated, so this usually means \
                 the network is disconnected and an island has no slack bus."
            ),
        }
    }
}

impl std::error::Error for PowerFlowError {}

impl From<CaseError> for PowerFlowError {
    fn from(e: CaseError) -> Self {
        Self::Case(e)
    }
}

impl From<SparseError> for PowerFlowError {
    fn from(e: SparseError) -> Self {
        Self::Linear(e)
    }
}

impl From<ReduceError> for PowerFlowError {
    fn from(e: ReduceError) -> Self {
        match e {
            ReduceError::Case(c) => Self::Case(c),
            ReduceError::Sparse(s) => Self::Linear(s),
        }
    }
}

/// A solved DC power flow.
#[derive(Debug, Clone, PartialEq)]
pub struct DcSolution {
    /// Bus voltage angles in radians, indexed as the case is. The slack bus
    /// is exactly zero.
    pub angles: Vec<f64>,
    /// Real power flow on each branch, per unit, measured at the from-end and
    /// positive in the from→to direction. Out-of-service branches are zero.
    pub flows: Vec<f64>,
    /// Real power the slack bus had to supply, per unit.
    ///
    /// DC power flow is lossless, so this is exactly the negative of every
    /// other bus's injection summed. It is reported rather than inferred
    /// because a slack injection far from what the modeller expected is the
    /// clearest sign the case is not what they thought.
    pub slack_injection: f64,
}

impl DcSolution {
    /// Branches loaded above their rating, as `(index, loading)` pairs where
    /// loading is the ratio of flow magnitude to rating.
    ///
    /// Unrated branches are skipped rather than reported as infinitely
    /// overloaded.
    #[must_use]
    pub fn violations(&self, case: &Case, threshold: f64) -> Vec<(usize, f64)> {
        let mut out = Vec::new();

        for (i, branch) in case.branches.iter().enumerate() {
            if !branch.in_service || branch.rating <= 0.0 {
                continue;
            }

            let loading = self.flows[i].abs() / branch.rating;
            if loading > threshold {
                out.push((i, loading));
            }
        }

        out
    }
}

/// Solves the DC power flow.
///
/// The formulation is the standard one. With losses dropped and voltages held
/// at 1.0 per unit, the real power leaving bus `i` toward bus `j` is
/// `(θᵢ − θⱼ) / xᵢⱼ`, so the nodal balance is `B θ = P` where `B` is the
/// susceptance matrix built from `1/x`.
///
/// `B` is singular by construction: adding a constant to every angle changes
/// no flow, so the null space is the all-ones vector. Fixing the slack angle
/// at zero and deleting its row and column removes exactly that degree of
/// freedom, which is why a case with no slack bus is rejected before we get
/// here rather than failing as an obscure pivot error.
///
/// # Errors
///
/// Returns [`PowerFlowError`] when the case is invalid or the reduced system
/// is singular.
pub fn solve_dc(case: &Case) -> Result<DcSolution, PowerFlowError> {
    case.validate()?;

    let reduction = Reduction::build(case)?;

    // A single-bus network is degenerate but legitimate: the slack supplies
    // everything and there is nothing to solve.
    if reduction.is_empty() {
        return Ok(DcSolution {
            angles: vec![0.0; case.n_bus()],
            flows: vec![0.0; case.n_branch()],
            slack_injection: -case.total_injection(),
        });
    }

    // A phase shifter forces power across itself independently of the angle
    // difference, so its contribution is removed from the scheduled injections
    // before the solve and added back into the flows afterwards.
    let shift = reduction.bus_shift_injection(case);

    let injections: Vec<f64> = reduction
        .full_of
        .iter()
        .map(|&bus| case.buses[bus].p_injection - shift[bus])
        .collect();

    let lu = DenseLu;
    let factors = lu.factor(&reduction.matrix)?;
    let angles = reduction.expand(&lu.solve(&factors, &injections)?);

    let flows = branch_flows(case, &angles);

    Ok(DcSolution {
        angles,
        flows,
        slack_injection: -case.total_injection(),
    })
}

/// Computes branch flows from a solved angle vector.
///
/// Separate from the solve because contingency screening reuses it: an LODF
/// update produces new flows without re-solving, and both paths must compute
/// the flow the same way or the screen and the verification disagree.
#[must_use]
pub fn branch_flows(case: &Case, angles: &[f64]) -> Vec<f64> {
    case.branches
        .iter()
        .map(|branch| {
            if branch.in_service {
                // The tap divides the susceptance and the phase shift offsets
                // the angle difference, so a shifter carries flow even when
                // its terminals are at the same angle.
                (angles[branch.from] - angles[branch.to] - branch.shift) / (branch.x * branch.tap)
            } else {
                0.0
            }
        })
        .collect()
}
