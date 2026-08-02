//! Line outage distribution factors.
//!
//! `LODF[l][k]` is the change in flow on branch `l` per unit of branch `k`'s
//! *pre-outage* flow, when `k` opens. So the post-contingency flow is
//!
//! ```text
//! f'[l] = f[l] + LODF[l][k] * f[k]
//! ```
//!
//! which is one multiply-add per branch. That is the two-tier N-1 screen in
//! one line: every contingency is evaluated without a factorization, the worst
//! are ranked, and only the top few go on to a full AC verification.
//!
//! The derivation is a rank-one update. Opening branch `k` is equivalent to
//! leaving it in place and injecting its flow at one end while withdrawing it
//! at the other, so the redistribution is a transfer sensitivity — with a
//! correction for the fact that the transfer also loads `k` itself, which is
//! where the `1 / (1 - ψ)` comes from.

use bs_case::Case;

use crate::ptdf::{FactorError, Ptdf};

/// Line outage distribution factors, `n_branch` by `n_branch`.
///
/// Column `k` is the redistribution caused by branch `k` opening.
#[derive(Debug, Clone, PartialEq)]
pub struct Lodf {
    n_branch: usize,
    /// Row-major: `values[monitored * n_branch + outaged]`.
    values: Vec<f64>,
    /// Why each branch can or cannot be screened, recorded at build time.
    ///
    /// Kept explicitly rather than inferred later from a zero column: a radial
    /// branch and an already-open one both leave zeros behind, and telling
    /// them apart afterwards means guessing from evidence that does not
    /// distinguish them.
    screenability: Vec<Screenability>,
}

/// Whether a branch's outage has a linear answer, and if not, why.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Screenability {
    /// The outage redistributes onto other branches and can be screened.
    Screenable,
    /// Opening the branch disconnects the network, so its flow has nowhere to
    /// go. A real contingency, but not one a linear factor can answer — it has
    /// to be simulated.
    Radial,
    /// The branch is already open, so it carries no flow and its outage is not
    /// a contingency at all.
    OutOfService,
}

/// How close `ψ` may come to 1 before the outage is treated as radial.
///
/// At exactly 1 the branch carries the whole transfer and the division is by
/// zero; just below it, the factors are finite but enormous and the linear
/// model has stopped describing anything. Both cases mean the same thing
/// physically — the branch is the only path — so both are reported as radial
/// rather than returned as a number that would be quoted downstream.
const RADIAL_TOLERANCE: f64 = 1e-9;

impl Lodf {
    /// Builds the factors for a case.
    ///
    /// # Errors
    ///
    /// Returns [`FactorError`] when the case is invalid or the reduced system
    /// is singular. A radial branch is *not* an error here — the whole matrix
    /// would be unusable if one radial line made it so. It is recorded in
    /// [`Lodf::is_screenable`] and reported by [`Lodf::outage`].
    pub fn build(case: &Case) -> Result<Self, FactorError> {
        Self::from_ptdf(case, &Ptdf::build(case)?)
    }

    /// Builds the factors from an existing PTDF, avoiding a second
    /// factorization when both are wanted.
    ///
    /// # Errors
    ///
    /// Returns [`FactorError`] when the case is invalid.
    ///
    /// # Panics
    ///
    /// Panics if the PTDF was built for a different case. Silently producing
    /// factors from a mismatched matrix would give plausible numbers for the
    /// wrong network, which is the one outcome worse than a crash.
    pub fn from_ptdf(case: &Case, ptdf: &Ptdf) -> Result<Self, FactorError> {
        let n_branch = case.n_branch();

        assert_eq!(
            ptdf.n_branch(),
            n_branch,
            "PTDF was built for a different case"
        );

        let mut values = vec![0.0; n_branch * n_branch];
        let mut screenability = vec![Screenability::OutOfService; n_branch];

        for (outaged, branch) in case.branches.iter().enumerate() {
            // An open branch carries no flow, so there is nothing to
            // redistribute and its column stays zero.
            if !branch.in_service {
                continue;
            }

            // Sensitivity of every branch to a transfer across the outaged
            // branch's own terminals. Opening it is equivalent to injecting
            // its flow at one end and withdrawing at the other.
            let psi = ptdf.transfer(branch.from, branch.to);

            // psi[outaged] is how much of that transfer the branch itself
            // takes. At 1 it takes all of it — nothing else connects the two
            // ends, so the branch is radial.
            let self_loading = psi[outaged];
            if (1.0 - self_loading).abs() <= RADIAL_TOLERANCE {
                screenability[outaged] = Screenability::Radial;
                continue;
            }

            screenability[outaged] = Screenability::Screenable;

            let scale = 1.0 / (1.0 - self_loading);

            for (monitored, &sensitivity) in psi.iter().enumerate() {
                values[monitored * n_branch + outaged] = sensitivity * scale;
            }

            // The outaged branch's own factor is exactly −1: after the outage
            // it carries nothing, so it loses precisely its pre-outage flow.
            // Written rather than computed, because the general formula gives
            // −1 only up to rounding and a screen that leaves 1e-16 on an open
            // branch invites a violation report on a branch that is not there.
            values[outaged * n_branch + outaged] = -1.0;
        }

        Ok(Self {
            n_branch,
            values,
            screenability,
        })
    }

    /// Number of branches.
    #[must_use]
    pub fn n_branch(&self) -> usize {
        self.n_branch
    }

    /// Whether branch `k`'s outage can be screened linearly, and if not, why.
    ///
    /// # Panics
    ///
    /// Panics if the index is out of range.
    #[must_use]
    pub fn screenability(&self, outaged: usize) -> Screenability {
        self.screenability[outaged]
    }

    /// Whether branch `k`'s outage can be screened linearly.
    ///
    /// # Panics
    ///
    /// Panics if the index is out of range.
    #[must_use]
    pub fn is_screenable(&self, outaged: usize) -> bool {
        self.screenability(outaged) == Screenability::Screenable
    }

    /// Change in flow on `monitored` per unit of `outaged`'s pre-outage flow.
    ///
    /// # Panics
    ///
    /// Panics if either index is out of range.
    #[must_use]
    pub fn get(&self, monitored: usize, outaged: usize) -> f64 {
        assert!(monitored < self.n_branch, "branch {monitored} out of range");
        assert!(outaged < self.n_branch, "branch {outaged} out of range");

        self.values[monitored * self.n_branch + outaged]
    }

    /// Post-contingency flows on every branch after `outaged` opens, given the
    /// pre-outage flows.
    ///
    /// This is the screen. One multiply-add per branch, no factorization, and
    /// exact for the DC model — a test compares it against re-solving with the
    /// branch removed and requires agreement.
    ///
    /// # Errors
    ///
    /// Returns [`FactorError::Radial`] when the outage disconnects the network
    /// and [`FactorError::OutOfService`] when the branch is already open.
    /// Neither has a linear answer, and returning one anyway is how a
    /// screening result becomes a wrong result rather than a missing one.
    ///
    /// # Panics
    ///
    /// Panics if `pre_outage` is not one entry per branch.
    pub fn outage(&self, outaged: usize, pre_outage: &[f64]) -> Result<Vec<f64>, FactorError> {
        assert_eq!(
            pre_outage.len(),
            self.n_branch,
            "pre-outage flows must have one entry per branch"
        );

        // Distinguished because they call for different responses: an open
        // branch is simply not a contingency, while a radial one is a real
        // contingency this method cannot answer and something else must.
        match self.screenability[outaged] {
            Screenability::Screenable => {}
            Screenability::Radial => return Err(FactorError::Radial { branch: outaged }),
            Screenability::OutOfService => {
                return Err(FactorError::OutOfService { branch: outaged });
            }
        }

        let shift = pre_outage[outaged];

        Ok((0..self.n_branch)
            .map(|monitored| pre_outage[monitored] + self.get(monitored, outaged) * shift)
            .collect())
    }
}
