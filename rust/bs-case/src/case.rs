//! The case itself, and the structural checks it must pass.

use std::fmt;

use bs_quantity::Base;

use crate::element::{Branch, Bus, BusKind};

/// A network case ready to solve.
#[derive(Debug, Clone, PartialEq)]
pub struct Case {
    /// Human-readable name, for logs and result provenance.
    pub name: String,
    /// The system per-unit base.
    pub base: Base,
    /// Buses, in canonical index order.
    pub buses: Vec<Bus>,
    /// Branches, in canonical index order.
    pub branches: Vec<Branch>,
}

/// Why a case is not solvable.
#[derive(Debug, Clone, PartialEq)]
pub enum CaseError {
    /// The case has no buses.
    Empty,
    /// No bus is marked slack, so the angles have no reference and the
    /// equations are singular.
    NoSlack,
    /// More than one slack bus. Each would fix an independent angle
    /// reference, and the network would be over-determined.
    MultipleSlack { count: usize },
    /// A branch references a bus index that does not exist.
    BranchOutOfRange { branch: usize, bus: usize },
    /// A branch connects a bus to itself.
    SelfLoop { branch: usize, bus: usize },
    /// A branch has zero reactance.
    ZeroReactance { branch: usize },
    /// A branch has a non-positive turns ratio, which has no physical meaning
    /// and divides the admittance by zero.
    BadTap { branch: usize, tap: f64 },
}

impl fmt::Display for CaseError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::Empty => write!(f, "case has no buses"),
            Self::NoSlack => write!(
                f,
                "case has no slack bus; without one the angles have no \
                 reference and the equations are singular"
            ),
            Self::MultipleSlack { count } => write!(
                f,
                "case has {count} slack buses; each fixes an independent angle \
                 reference, so the network is over-determined"
            ),
            Self::BranchOutOfRange { branch, bus } => {
                write!(
                    f,
                    "branch {branch} references bus {bus}, which does not exist"
                )
            }
            Self::SelfLoop { branch, bus } => {
                write!(f, "branch {branch} connects bus {bus} to itself")
            }
            Self::ZeroReactance { branch } => write!(
                f,
                "branch {branch} has zero reactance; a zero-impedance branch is \
                 a bus merge that topology processing should have collapsed"
            ),
            Self::BadTap { branch, tap } => write!(
                f,
                "branch {branch} has turns ratio {tap}; a ratio must be \
                 positive, and 1.0 is the value for a line with no transformer"
            ),
        }
    }
}

impl std::error::Error for CaseError {}

impl Case {
    /// Builds a case.
    #[must_use]
    pub fn new(
        name: impl Into<String>,
        base: Base,
        buses: Vec<Bus>,
        branches: Vec<Branch>,
    ) -> Self {
        Self {
            name: name.into(),
            base,
            buses,
            branches,
        }
    }

    /// Number of buses.
    #[must_use]
    pub fn n_bus(&self) -> usize {
        self.buses.len()
    }

    /// Number of branches, in service or not.
    #[must_use]
    pub fn n_branch(&self) -> usize {
        self.branches.len()
    }

    /// Index of the slack bus.
    ///
    /// # Errors
    ///
    /// Returns [`CaseError::NoSlack`] or [`CaseError::MultipleSlack`].
    pub fn slack_index(&self) -> Result<usize, CaseError> {
        let mut found = None;
        let mut count = 0;

        for (i, bus) in self.buses.iter().enumerate() {
            if bus.kind == BusKind::Slack {
                count += 1;
                if found.is_none() {
                    found = Some(i);
                }
            }
        }

        match (found, count) {
            (Some(i), 1) => Ok(i),
            (_, 0) => Err(CaseError::NoSlack),
            (_, n) => Err(CaseError::MultipleSlack { count: n }),
        }
    }

    /// Checks the case is structurally solvable.
    ///
    /// This runs before assembly rather than letting the factorization fail,
    /// because "singular at pivot 7" is a much worse diagnosis than "branch 3
    /// references a bus that does not exist" for the same underlying mistake.
    ///
    /// # Errors
    ///
    /// Returns the first [`CaseError`] found.
    pub fn validate(&self) -> Result<(), CaseError> {
        if self.buses.is_empty() {
            return Err(CaseError::Empty);
        }

        self.slack_index()?;

        for (i, branch) in self.branches.iter().enumerate() {
            for bus in [branch.from, branch.to] {
                if bus >= self.buses.len() {
                    return Err(CaseError::BranchOutOfRange { branch: i, bus });
                }
            }

            if branch.from == branch.to {
                return Err(CaseError::SelfLoop {
                    branch: i,
                    bus: branch.from,
                });
            }

            if branch.x == 0.0 {
                return Err(CaseError::ZeroReactance { branch: i });
            }

            if !(branch.tap > 0.0) {
                return Err(CaseError::BadTap {
                    branch: i,
                    tap: branch.tap,
                });
            }
        }

        Ok(())
    }

    /// Total real power injection across all buses, per unit.
    ///
    /// In a converged DC solution this is exactly zero at the slack bus's
    /// expense: DC power flow is lossless, so generation equals load. Checking
    /// it is an oracle-independent test — it catches "my solver and my golden
    /// are wrong the same way", which comparison against a reference tool
    /// structurally cannot.
    #[must_use]
    pub fn total_injection(&self) -> f64 {
        self.buses.iter().map(|b| b.p_injection).sum()
    }

    /// Returns the indices of in-service branches, in case order.
    #[must_use]
    pub fn live_branches(&self) -> Vec<usize> {
        (0..self.branches.len())
            .filter(|&i| self.branches[i].in_service)
            .collect()
    }
}
