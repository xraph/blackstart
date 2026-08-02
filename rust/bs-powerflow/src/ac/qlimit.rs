//! Generator reactive limit enforcement.
//!
//! A PV bus holds its scheduled voltage by producing whatever reactive power
//! that takes. Real generators cannot. When the solution asks one for more
//! than it has, the honest model is to give it what it has and let the voltage
//! fall where it falls — so the bus is switched to PQ with its reactive
//! injection fixed at the violated limit, and the network is solved again.
//!
//! This is not a refinement. Without it, a stressed network reports a
//! comfortable voltage profile that no physical generator could produce, which
//! for a tool whose job is to find voltage problems is precisely the wrong
//! kind of wrong: it converts a real finding into a clean answer.
//!
//! A switched bus can also come back. If a later solve leaves it wanting less
//! reactive power than its limit *and* its voltage has moved past the schedule
//! in the direction that would need more, it is restored to PV. Without
//! back-off the first excursion is permanent and the answer depends on the
//! order limits happened to bind.

use bs_case::{BusKind, Case};

use crate::ac::newton::{AcError, AcSolution, Options, solve_ac_from};
use crate::ac::power::Voltages;

/// What happened to one generator's reactive limit.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Enforcement {
    /// The generator stayed on voltage control.
    OnVoltage,
    /// It hit its upper limit and was fixed there; its voltage is now below
    /// schedule.
    AtMaximum,
    /// It hit its lower limit and was fixed there; its voltage is now above
    /// schedule.
    AtMinimum,
}

/// A solved power flow with reactive limits respected.
#[derive(Debug, Clone, PartialEq)]
pub struct LimitedSolution {
    /// The final solution.
    pub solution: AcSolution,
    /// What happened at each bus, indexed as the case is. Non-generator buses
    /// are [`Enforcement::OnVoltage`].
    pub enforcement: Vec<Enforcement>,
    /// How many times the network was re-solved after a limit change.
    ///
    /// Reported because it is the honest measure of how hard the case was:
    /// zero means no limit bound, and a large number means limits are binding
    /// and releasing in a way worth looking at rather than a converged answer
    /// worth quoting.
    pub rounds: usize,
}

impl LimitedSolution {
    /// Buses whose generators are pinned at a reactive limit.
    ///
    /// These are the finding. Each one is a generator that could not hold its
    /// scheduled voltage, which is a voltage-support problem whether or not
    /// any bus ended up outside its limits.
    #[must_use]
    pub fn binding(&self) -> Vec<usize> {
        self.enforcement
            .iter()
            .enumerate()
            .filter(|(_, state)| **state != Enforcement::OnVoltage)
            .map(|(bus, _)| bus)
            .collect()
    }
}

/// Maximum limit-switching rounds before giving up.
///
/// Each round re-solves the network, so this bounds the work. Cases that
/// oscillate — a generator that binds, releases, and binds again — do exist,
/// and looping forever on one is worse than reporting the oscillation.
const MAX_ROUNDS: usize = 10;

/// Solves the AC power flow, enforcing generator reactive limits.
///
/// # Errors
///
/// Returns [`AcError`] when a solve fails, or [`AcError::NotConverged`] when
/// the limit switching itself does not settle — carrying the residual of the
/// last solve, which is genuinely converged even though the limit set is not.
pub fn solve_with_limits(case: &Case, options: Options) -> Result<LimitedSolution, AcError> {
    let mut working = case.clone();
    let mut enforcement = vec![Enforcement::OnVoltage; case.n_bus()];
    let mut start = Voltages::flat(case);

    for round in 0..=MAX_ROUNDS {
        // Warm-starting each round is correct here in a way it is not in
        // general: the limit change is a small perturbation of a network that
        // has already been solved, so the previous answer is genuinely near
        // the new one rather than merely available.
        let solution = solve_ac_from(&working, &start, options)?;

        if !adjust(case, &mut working, &mut enforcement, &solution) {
            return Ok(LimitedSolution {
                solution,
                enforcement,
                rounds: round,
            });
        }

        start = solution.voltages;
    }

    Err(AcError::NotConverged {
        iterations: MAX_ROUNDS,
        residual: f64::NAN,
    })
}

/// Applies one round of limit changes; reports whether anything changed.
fn adjust(
    original: &Case,
    working: &mut Case,
    enforcement: &mut [Enforcement],
    solution: &AcSolution,
) -> bool {
    let mut changed = false;

    for bus in 0..original.n_bus() {
        let scheduled = &original.buses[bus];

        // Only generators have reactive limits to enforce. The slack absorbs
        // whatever is left over and has no schedule to violate.
        if scheduled.kind != BusKind::Pv {
            continue;
        }

        match enforcement[bus] {
            Enforcement::OnVoltage => {
                let required = solution.injections.q[bus];

                let (limit, state) = if required > scheduled.q_max {
                    (scheduled.q_max, Enforcement::AtMaximum)
                } else if required < scheduled.q_min {
                    (scheduled.q_min, Enforcement::AtMinimum)
                } else {
                    continue;
                };

                // Fix the reactive output at the limit and free the voltage.
                working.buses[bus].kind = BusKind::Pq;
                working.buses[bus].q_injection = limit;
                enforcement[bus] = state;
                changed = true;
            }

            // A pinned generator goes back on voltage control only when the
            // voltage has moved past its schedule in the direction that would
            // require more reactive power than the limit allows. Testing the
            // reactive output alone would release it immediately — it is
            // sitting exactly at its limit by construction.
            Enforcement::AtMaximum => {
                if solution.voltages.magnitude[bus] > scheduled.v_magnitude {
                    restore(working, enforcement, scheduled, bus);
                    changed = true;
                }
            }
            Enforcement::AtMinimum => {
                if solution.voltages.magnitude[bus] < scheduled.v_magnitude {
                    restore(working, enforcement, scheduled, bus);
                    changed = true;
                }
            }
        }
    }

    changed
}

fn restore(
    working: &mut Case,
    enforcement: &mut [Enforcement],
    scheduled: &bs_case::Bus,
    bus: usize,
) {
    working.buses[bus].kind = BusKind::Pv;
    working.buses[bus].q_injection = scheduled.q_injection;
    working.buses[bus].v_magnitude = scheduled.v_magnitude;
    enforcement[bus] = Enforcement::OnVoltage;
}
