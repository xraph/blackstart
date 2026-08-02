//! The Newton-Raphson iteration.
//!
//! Evaluate the mismatch, assemble the Jacobian, solve for a step, apply it,
//! repeat. Newton converges quadratically near the solution, so on a
//! well-conditioned transmission network from a flat start this takes three to
//! five iterations and the iteration count is itself diagnostic — a case that
//! suddenly needs twelve has changed in a way worth knowing about.

use bs_case::{Case, CaseError};
use bs_sparse::{DenseLu, Error as SparseError, Factorize};

use crate::ac::jacobian;
use crate::ac::layout::Layout;
use crate::ac::power::{Injections, Voltages, injections, max_mismatch, mismatch};
use crate::dc::branch_flows;
use crate::ybus::{Ybus, YbusError};

/// How the iteration is bounded.
#[derive(Debug, Clone, Copy, PartialEq)]
pub struct Options {
    /// Convergence threshold on the largest bus mismatch, per unit.
    ///
    /// 1e-8 per unit on a 100 MVA base is a microwatt — far below any
    /// modelling error, so a converged answer is limited by the model rather
    /// than by the iteration.
    pub tolerance: f64,
    /// Maximum iterations before giving up.
    ///
    /// Twenty is generous for a method that normally takes four. A case that
    /// reaches this limit has not converged slowly, it has diverged or is
    /// oscillating, and more iterations will not help.
    pub max_iterations: usize,
}

impl Default for Options {
    fn default() -> Self {
        Self {
            tolerance: 1e-8,
            max_iterations: 20,
        }
    }
}

/// A solved AC power flow.
#[derive(Debug, Clone, PartialEq)]
pub struct AcSolution {
    /// Bus voltages.
    pub voltages: Voltages,
    /// Power injected at every bus by the network. At the slack this is what
    /// the reference had to supply; at a PV bus, the reactive part is what the
    /// generator had to produce to hold its scheduled voltage.
    pub injections: Injections,
    /// Real power flow on each branch, per unit, measured at the from-end and
    /// positive in the from→to direction.
    ///
    /// Computed the DC way — `(θᵢ − θⱼ)/x` — so that AC and DC flows are
    /// directly comparable and a screen can be checked against a verification
    /// without one of them silently measuring a different quantity. The AC
    /// branch flow including losses is a separate question.
    pub flows: Vec<f64>,
    /// Iterations taken. Part of the result rather than a log line, because a
    /// golden file that captures convergence behaviour catches a change in the
    /// Jacobian that still lands on the same answer.
    pub iterations: usize,
    /// The largest bus mismatch at convergence, per unit.
    pub residual: f64,
}

/// Why an AC power flow did not solve.
#[derive(Debug, Clone, PartialEq)]
pub enum AcError {
    /// The case itself is not solvable.
    Case(CaseError),
    /// The admittance matrix could not be assembled.
    Ybus(YbusError),
    /// The Jacobian was singular. Usually a disconnected island, or a network
    /// so heavily loaded that no solution exists at this operating point.
    Linear(SparseError),
    /// The iteration limit was reached.
    ///
    /// Carries the residual it reached and the count, because "did not
    /// converge" is very different at a residual of 1e-6 — nearly there, worth
    /// a tighter limit — than at 1e3, which means the case has no solution
    /// near the starting point.
    NotConverged { iterations: usize, residual: f64 },
    /// Reactive-limit switching never settled: generators bound and released
    /// in a cycle.
    ///
    /// Distinct from [`AcError::NotConverged`] because every underlying solve converged —
    /// the equations are fine and the switching heuristic is what failed, which
    /// calls for a different response and a different fix.
    LimitCycle { rounds: usize },
}

impl core::fmt::Display for AcError {
    fn fmt(&self, f: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        match self {
            Self::Case(e) => write!(f, "case: {e}"),
            Self::Ybus(e) => write!(f, "admittance: {e}"),
            Self::Linear(e) => write!(
                f,
                "singular Jacobian: {e}. Usually a disconnected island, or a \
                 network loaded past the point where a solution exists."
            ),
            Self::NotConverged {
                iterations,
                residual,
            } => write!(
                f,
                "did not converge in {iterations} iterations; largest bus \
                 mismatch is {residual} per unit"
            ),
            Self::LimitCycle { rounds } => write!(
                f,
                "generator reactive limits did not settle in {rounds} rounds; \
                 machines are binding and releasing in a cycle. Every power \
                 flow converged, so the equations are fine and the switching \
                 heuristic is what failed."
            ),
        }
    }
}

impl std::error::Error for AcError {}

impl From<CaseError> for AcError {
    fn from(e: CaseError) -> Self {
        Self::Case(e)
    }
}

impl From<YbusError> for AcError {
    fn from(e: YbusError) -> Self {
        Self::Ybus(e)
    }
}

impl From<SparseError> for AcError {
    fn from(e: SparseError) -> Self {
        Self::Linear(e)
    }
}

/// Solves the AC power flow from a flat start with default options.
///
/// # Errors
///
/// Returns [`AcError`] when the case is invalid, the Jacobian is singular, or
/// the iteration does not converge.
pub fn solve_ac(case: &Case) -> Result<AcSolution, AcError> {
    solve_ac_from(case, &Voltages::flat(case), Options::default())
}

/// Solves the AC power flow from a given starting state.
///
/// The start is explicit rather than hidden so that a caller who genuinely
/// wants to warm-start — a contingency verification reusing the base case
/// answer, where the two states really are close — can, while the default path
/// stays flat and therefore reproducible regardless of what was solved before.
///
/// # Errors
///
/// Returns [`AcError`] when the case is invalid, the Jacobian is singular, or
/// the iteration does not converge.
pub fn solve_ac_from(
    case: &Case,
    start: &Voltages,
    options: Options,
) -> Result<AcSolution, AcError> {
    case.validate()?;

    let ybus = Ybus::build(case)?;
    let layout = Layout::build(case)?;

    let mut volts = start.clone();

    // A single-bus case has no unknowns. Reporting it as converged in zero
    // iterations is honest: there was nothing to solve, and the alternative is
    // a factorization of an empty matrix.
    if layout.is_empty() {
        let power = injections(&ybus, &volts);
        return Ok(AcSolution {
            flows: vec![0.0; case.n_branch()],
            voltages: volts,
            injections: power,
            iterations: 0,
            residual: 0.0,
        });
    }

    let lu = DenseLu;
    let mut power = injections(&ybus, &volts);
    let mut residual = max_mismatch(&mismatch(case, &layout, &power));

    for iteration in 1..=options.max_iterations {
        if residual <= options.tolerance {
            return Ok(finish(case, volts, power, iteration - 1, residual));
        }

        let f = mismatch(case, &layout, &power);
        let matrix = jacobian::build(&ybus, &layout, &volts, &power)?;

        // J Δx = mismatch, with no negation: the mismatch is already
        // `scheduled − computed`, which is the negative of the residual Newton
        // drives to zero, so the two sign flips cancel. See jacobian's module
        // documentation — this is the only place the convention matters.
        let step = lu.solve(&lu.factor(&matrix)?, &f)?;

        layout.apply(&step, &mut volts.angle, &mut volts.magnitude);

        power = injections(&ybus, &volts);
        residual = max_mismatch(&mismatch(case, &layout, &power));
    }

    // Checked once more after the final update: an iteration that converges on
    // its last permitted step has converged, and reporting otherwise would be
    // an off-by-one dressed up as a numerical failure.
    if residual <= options.tolerance {
        return Ok(finish(case, volts, power, options.max_iterations, residual));
    }

    Err(AcError::NotConverged {
        iterations: options.max_iterations,
        residual,
    })
}

fn finish(
    case: &Case,
    volts: Voltages,
    power: Injections,
    iterations: usize,
    residual: f64,
) -> AcSolution {
    let flows = branch_flows(case, &volts.angle);

    AcSolution {
        voltages: volts,
        injections: power,
        flows,
        iterations,
        residual,
    }
}
