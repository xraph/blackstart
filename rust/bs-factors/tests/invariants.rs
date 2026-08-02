//! Oracle-independent checks on the distribution factors.
//!
//! These hold regardless of what pandapower or MATPOWER says, because they
//! compare the factors against the solve they were derived from and against
//! their own algebraic properties. That catches the failure mode oracle
//! comparison structurally cannot: a factor matrix and its golden file being
//! wrong in the same way.
//!
//! The exact-zero comparisons are deliberate. The slack column and a
//! self-transfer are zero *by construction* — never written, or subtracted
//! from themselves — so a value merely near zero would mean arithmetic reached
//! somewhere it should not have, and an epsilon would hide it.
#![allow(clippy::float_cmp)]

use bs_case::{Branch, Bus, Case};
use bs_factors::Ptdf;
use bs_powerflow::solve_dc;
use bs_quantity::Base;

fn base() -> Base {
    Base::new(100.0, 138.0).unwrap()
}

fn close(got: f64, want: f64, tol: f64) -> bool {
    (got - want).abs() <= tol * want.abs().max(1.0)
}

/// A four-bus mesh. Meshed rather than radial because a radial network hides
/// loop flow, and loop flow is the entire subject of a distribution factor.
fn mesh() -> Case {
    Case::new(
        "mesh",
        base(),
        vec![
            Bus::slack("s", 1.0),
            Bus::load("a", -0.4, 0.0),
            Bus::load("b", -0.7, 0.0),
            Bus::generator("g", 0.3, 1.0),
        ],
        vec![
            Branch::new("s-a", 0, 1, 0.01, 0.10),
            Branch::new("a-b", 1, 2, 0.01, 0.15),
            Branch::new("b-g", 2, 3, 0.01, 0.12),
            Branch::new("g-s", 3, 0, 0.01, 0.08),
            Branch::new("a-g", 1, 3, 0.01, 0.20),
        ],
    )
}

/// The check the whole screen rests on. If the factors do not reproduce the
/// solve, every contingency ranking built on them is measuring something else.
///
/// Oracle-independent: two paths through this repository, compared against
/// each other rather than against pandapower.
#[test]
fn ptdf_reproduces_the_solve() {
    let case = mesh();

    let solved = solve_dc(&case).unwrap();
    let ptdf = Ptdf::build(&case).unwrap();

    let injections: Vec<f64> = case.buses.iter().map(|b| b.p_injection).collect();
    let from_factors = ptdf.flows(&injections);

    for (branch, (&factored, &solved_flow)) in from_factors.iter().zip(&solved.flows).enumerate() {
        assert!(
            close(factored, solved_flow, 1e-10),
            "branch {branch}: factors say {factored}, the solve says {solved_flow}"
        );
    }
}

/// Injecting at the slack and withdrawing at the slack is the null transfer.
/// The column is zero by definition, and left at zero rather than computed,
/// so this also asserts it was not overwritten by arithmetic that lands near
/// but not on zero.
#[test]
fn the_slack_column_is_exactly_zero() {
    let ptdf = Ptdf::build(&mesh()).unwrap();
    let slack = ptdf.slack();

    for branch in 0..ptdf.n_branch() {
        assert_eq!(
            ptdf.get(branch, slack),
            0.0,
            "branch {branch} has a non-zero slack sensitivity"
        );
    }
}

#[test]
fn a_transfer_to_itself_moves_nothing() {
    let ptdf = Ptdf::build(&mesh()).unwrap();

    for factor in ptdf.transfer(2, 2) {
        assert_eq!(factor, 0.0);
    }
}

#[test]
fn transfers_are_antisymmetric() {
    let ptdf = Ptdf::build(&mesh()).unwrap();

    let forward = ptdf.transfer(1, 3);
    let backward = ptdf.transfer(3, 1);

    for (branch, (f, b)) in forward.iter().zip(&backward).enumerate() {
        assert!(
            close(*f, -*b, 1e-12),
            "branch {branch}: {f} is not the negative of {b}"
        );
    }
}

/// A transfer factor must not depend on which bus was chosen as the angle
/// reference, even though the columns it is built from do. Re-solving the same
/// network with a different slack has to give the same transfer sensitivities.
///
/// This is the property that makes a transfer factor meaningful at all, and it
/// is not visible from any single PTDF matrix.
#[test]
fn transfers_do_not_depend_on_the_choice_of_slack() {
    let mut case = mesh();
    let with_bus0 = Ptdf::build(&case).unwrap().transfer(1, 2);

    // Move the reference to bus 3. Injections stay as they were; only the
    // angle reference moves.
    case.buses[0] = Bus::load("s", 0.0, 0.0);
    case.buses[3] = Bus::slack("g", 1.0);

    let with_bus3 = Ptdf::build(&case).unwrap().transfer(1, 2);

    for (branch, (a, b)) in with_bus0.iter().zip(&with_bus3).enumerate() {
        assert!(
            close(*a, *b, 1e-10),
            "branch {branch}: slack at bus 0 gives {a}, slack at bus 3 gives {b}"
        );
    }
}

/// Building the factors twice must give bit-identical matrices, for the same
/// reason the solve must: a golden comparison over a matrix that shifts in its
/// last bits is measuring noise.
#[test]
fn repeated_builds_are_bit_identical() {
    let case = mesh();
    let first = Ptdf::build(&case).unwrap();

    for _ in 0..10 {
        assert_eq!(first, Ptdf::build(&case).unwrap());
    }
}

#[test]
fn an_out_of_service_branch_has_no_sensitivity() {
    let mut case = mesh();
    case.branches[1].in_service = false;

    let ptdf = Ptdf::build(&case).unwrap();

    for bus in 0..ptdf.n_bus() {
        assert_eq!(ptdf.get(1, bus), 0.0, "bus {bus}");
    }

    // The index space is unchanged, so a contingency can restore the branch
    // without rebuilding every index.
    assert_eq!(ptdf.n_branch(), case.n_branch());
}
