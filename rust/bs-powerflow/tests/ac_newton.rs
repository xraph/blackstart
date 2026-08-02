//! The Newton-Raphson AC solve.
//!
//! Convergence proves nothing on its own — an iteration can converge neatly to
//! the wrong point. What is checked here is that the converged state really is
//! a solution of the equations it claims to solve, that the quantities it was
//! told to hold fixed did not move, and that it behaves the way Newton is
//! supposed to. The physical conservation laws are in `ac_conservation.rs`.

use bs_case::{Branch, Bus, BusKind, Case};
use bs_powerflow::Ybus;
use bs_powerflow::ac::{AcError, Options, Voltages, injections, solve_ac, solve_ac_from};
use bs_quantity::Base;

fn base() -> Base {
    Base::new(100.0, 138.0).unwrap()
}

fn close(got: f64, want: f64, tol: f64) -> bool {
    (got - want).abs() <= tol * want.abs().max(1.0)
}

/// Slack, two PQ buses and a PV bus, meshed and resistive so losses are real.
fn mixed() -> Case {
    Case::new(
        "mixed",
        base(),
        vec![
            Bus::slack("s", 1.0),
            Bus::load("a", -0.4, -0.1),
            Bus::load("b", -0.7, -0.2),
            Bus::generator("g", 0.3, 1.02),
        ],
        vec![
            Branch::new("s-a", 0, 1, 0.02, 0.10),
            Branch::new("a-b", 1, 2, 0.03, 0.15),
            Branch::new("b-g", 2, 3, 0.02, 0.12),
            Branch::new("g-s", 3, 0, 0.01, 0.08),
            Branch::new("a-g", 1, 3, 0.04, 0.20),
        ],
    )
}

/// The converged state must actually satisfy the power-flow equations. This
/// re-evaluates them from the reported voltages, independently of whatever the
/// iteration believed at the end, and requires every scheduled bus to balance.
///
/// It is the difference between "the loop exited" and "the answer is right".
#[test]
fn the_converged_state_satisfies_the_equations() {
    let case = mixed();
    let solution = solve_ac(&case).unwrap();

    let recomputed = injections(&Ybus::build(&case).unwrap(), &solution.voltages);
    let slack = case.slack_index().unwrap();

    for (bus, spec) in case.buses.iter().enumerate() {
        if bus == slack {
            continue;
        }

        assert!(
            close(recomputed.p[bus], spec.p_injection, 1e-7),
            "bus {bus}: real power settled at {} but {} was scheduled",
            recomputed.p[bus],
            spec.p_injection
        );

        // A PV bus's reactive output is free — it produces whatever holds its
        // voltage — so only PQ buses have a reactive schedule to meet.
        if spec.kind == BusKind::Pq {
            assert!(
                close(recomputed.q[bus], spec.q_injection, 1e-7),
                "bus {bus}: reactive power settled at {} but {} was scheduled",
                recomputed.q[bus],
                spec.q_injection
            );
        }
    }

    assert!(solution.residual <= 1e-8, "residual {}", solution.residual);
}

/// Every bus that fixes a voltage must still be at it. The slack and every PV
/// bus hold their magnitudes by definition, and a solver that quietly moved
/// one would produce a plausible answer to a different question.
#[test]
fn fixed_voltages_stay_fixed() {
    let case = mixed();
    let solution = solve_ac(&case).unwrap();

    for (bus, spec) in case.buses.iter().enumerate() {
        if spec.kind == BusKind::Pq {
            continue;
        }
        assert!(
            close(solution.voltages.magnitude[bus], spec.v_magnitude, 1e-12),
            "bus {bus} was scheduled at {} but settled at {}",
            spec.v_magnitude,
            solution.voltages.magnitude[bus]
        );
    }

    // The slack is the angle reference and must not have moved.
    assert!(close(
        solution.voltages.angle[case.slack_index().unwrap()],
        0.0,
        1e-12
    ));
}

/// Newton converges quadratically near the solution, so a well-conditioned
/// case from a flat start should take a handful of iterations. A sudden jump
/// is the signature of a Jacobian that has become approximate — which still
/// converges, just slowly, and would otherwise go unnoticed.
#[test]
fn a_well_conditioned_case_converges_quickly() {
    let solution = solve_ac(&mixed()).unwrap();

    assert!(
        solution.iterations <= 6,
        "took {} iterations from a flat start; the Jacobian may have drifted \
         from the true derivative",
        solution.iterations
    );
    assert!(
        solution.iterations >= 1,
        "reported zero iterations for real work"
    );
}

/// Warm-starting from a nearby state must reach the same answer in fewer
/// iterations. If it reached a *different* answer, the solve would depend on
/// what was solved before it.
#[test]
fn a_warm_start_reaches_the_same_answer_sooner() {
    let case = mixed();
    let cold = solve_ac(&case).unwrap();

    let warm = solve_ac_from(&case, &cold.voltages, Options::default()).unwrap();

    assert_eq!(warm.iterations, 0, "starting at the answer still iterated");

    for bus in 0..case.n_bus() {
        assert!(close(
            warm.voltages.angle[bus],
            cold.voltages.angle[bus],
            1e-9
        ));
        assert!(close(
            warm.voltages.magnitude[bus],
            cold.voltages.magnitude[bus],
            1e-9
        ));
    }
}

/// Repeated solves must be bit-identical, for the same reason the DC solve
/// must: a golden file over a result that shifts in its last bits measures
/// noise. The iteration count is part of that — it is in the result precisely
/// so a change in convergence behaviour is caught even when the answer lands
/// in the same place.
#[test]
fn repeated_solves_are_bit_identical() {
    let case = mixed();
    let first = solve_ac(&case).unwrap();

    for _ in 0..10 {
        assert_eq!(first, solve_ac(&case).unwrap());
    }
}

/// A case with no solution near the start must say so, with the residual it
/// reached — "did not converge" at 1e-6 means try a tighter limit, at 1e3 it
/// means the network has no solution at this loading, and the two call for
/// completely different responses.
#[test]
fn a_hopeless_case_reports_what_it_reached() {
    let case = mixed();

    // One iteration is not enough from flat on a case that needs several.
    let err = solve_ac_from(
        &case,
        &Voltages::flat(&case),
        Options {
            tolerance: 1e-12,
            max_iterations: 1,
        },
    )
    .unwrap_err();

    match err {
        AcError::NotConverged {
            iterations,
            residual,
        } => {
            assert_eq!(iterations, 1);
            assert!(residual > 1e-12, "reported failure at a converged residual");
            assert!(residual.is_finite(), "residual was {residual}");
        }
        other => panic!("expected NotConverged, got {other:?}"),
    }

    assert!(
        AcError::NotConverged {
            iterations: 20,
            residual: 0.4
        }
        .to_string()
        .contains("0.4"),
        "the message must carry the residual"
    );
}

#[test]
fn an_invalid_case_is_rejected_before_iterating() {
    let case = Case::new(
        "no-slack",
        base(),
        vec![Bus::load("a", -0.5, 0.0), Bus::load("b", 0.5, 0.0)],
        vec![Branch::new("l", 0, 1, 0.01, 0.1)],
    );

    assert!(matches!(solve_ac(&case), Err(AcError::Case(_))));
}
