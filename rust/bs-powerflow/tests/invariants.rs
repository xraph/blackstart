//! Oracle-independent checks on the DC solution.
//!
//! Power balance, nodal balance, linearity and determinism all hold
//! regardless of what any reference tool says. They catch the failure mode
//! that comparison against pandapower or MATPOWER structurally cannot — a
//! solver and its golden file being wrong in the same way, which is what
//! happens when a golden is regenerated from the code it is meant to check.
//!
//! The structural-rejection tests are here too: a case that cannot be solved
//! must be named as a modelling mistake, not surface as a pivot failure.

use bs_case::{Branch, Bus, Case, CaseError};
use bs_powerflow::{PowerFlowError, solve_dc};
use bs_quantity::Base;

fn base() -> Base {
    Base::new(100.0, 138.0).unwrap()
}

fn close(got: f64, want: f64, tol: f64) -> bool {
    (got - want).abs() <= tol * want.abs().max(1.0)
}

/// DC power flow is lossless, so what leaves the slack equals what every other
/// bus draws. This is oracle-independent: it holds regardless of what any
/// reference tool says, and it fails if the assembly drops or double-counts a
/// term.
#[test]
fn power_balances_exactly() {
    let case = mesh_case();
    let sol = solve_dc(&case).unwrap();

    let drawn: f64 = case.buses.iter().map(|b| b.p_injection).sum();

    assert!(
        close(sol.slack_injection, -drawn, 1e-12),
        "slack supplies {} but the network draws {}",
        sol.slack_injection,
        -drawn
    );
}

/// Every bus's net flow must equal its injection. This is the nodal balance
/// the solve was supposed to enforce, checked independently of how it was
/// enforced.
#[test]
fn every_bus_balances() {
    let case = mesh_case();
    let sol = solve_dc(&case).unwrap();

    let mut net = vec![0.0; case.n_bus()];

    for (i, branch) in case.branches.iter().enumerate() {
        if !branch.in_service {
            continue;
        }
        net[branch.from] += sol.flows[i];
        net[branch.to] -= sol.flows[i];
    }

    let slack = case.slack_index().unwrap();

    for (bus, &exported) in net.iter().enumerate() {
        if bus == slack {
            continue;
        }
        assert!(
            close(exported, case.buses[bus].p_injection, 1e-10),
            "bus {bus} exports {exported} but injects {}",
            case.buses[bus].p_injection
        );
    }
}

/// Taking a branch out must change the answer and must not corrupt the index
/// space — the branch stays in the case so contingency analysis can restore it
/// without rebuilding every index.
#[test]
fn an_out_of_service_branch_carries_no_flow() {
    let mut case = mesh_case();

    let before = solve_dc(&case).unwrap();
    assert!(
        before.flows[1].abs() > 1e-6,
        "precondition: branch 1 carries flow"
    );

    case.branches[1].in_service = false;
    let after = solve_dc(&case).unwrap();

    assert!(close(after.flows[1], 0.0, 1e-12));
    assert_eq!(after.flows.len(), before.flows.len(), "index space changed");

    // And the power still balances with the branch gone.
    let drawn: f64 = case.buses.iter().map(|b| b.p_injection).sum();
    assert!(close(after.slack_injection, -drawn, 1e-12));
}

/// The whole solution scales linearly, which is the defining property of the
/// linearization and the reason distribution factors work at all.
#[test]
fn the_solution_is_linear_in_injection() {
    let case = mesh_case();
    let single = solve_dc(&case).unwrap();

    let mut doubled = case.clone();
    for bus in &mut doubled.buses {
        bus.p_injection *= 2.0;
    }

    let double = solve_dc(&doubled).unwrap();

    for i in 0..case.n_branch() {
        assert!(
            close(double.flows[i], 2.0 * single.flows[i], 1e-10),
            "branch {i}: {} is not twice {}",
            double.flows[i],
            single.flows[i]
        );
    }
}

/// The same case solved twice must give bit-identical answers. Determinism is
/// a correctness property here, not a nicety: golden-file comparison is
/// meaningless without it.
#[test]
fn repeated_solves_are_bit_identical() {
    let case = mesh_case();
    let first = solve_dc(&case).unwrap();

    for _ in 0..20 {
        let again = solve_dc(&case).unwrap();
        assert_eq!(
            first, again,
            "two solves of one case disagreed; golden comparison would be noise"
        );
    }
}

#[test]
fn a_case_with_no_slack_is_rejected_before_the_solve() {
    let case = Case::new(
        "no-slack",
        base(),
        vec![Bus::load("a", -0.5, 0.0), Bus::load("b", 0.5, 0.0)],
        vec![Branch::new("l", 0, 1, 0.0, 0.1)],
    );

    // Rejected as a case error, not as an obscure pivot failure — the two
    // point at completely different investigations.
    assert_eq!(
        solve_dc(&case),
        Err(PowerFlowError::Case(CaseError::NoSlack))
    );
}

#[test]
fn structural_mistakes_are_named() {
    let bad = [
        (
            Case::new(
                "two-slack",
                base(),
                vec![Bus::slack("a", 1.0), Bus::slack("b", 1.0)],
                vec![Branch::new("l", 0, 1, 0.0, 0.1)],
            ),
            CaseError::MultipleSlack { count: 2 },
        ),
        (
            Case::new(
                "dangling",
                base(),
                vec![Bus::slack("a", 1.0)],
                vec![Branch::new("l", 0, 7, 0.0, 0.1)],
            ),
            CaseError::BranchOutOfRange { branch: 0, bus: 7 },
        ),
        (
            Case::new(
                "self-loop",
                base(),
                vec![Bus::slack("a", 1.0), Bus::load("b", 0.0, 0.0)],
                vec![Branch::new("l", 1, 1, 0.0, 0.1)],
            ),
            CaseError::SelfLoop { branch: 0, bus: 1 },
        ),
        (
            Case::new(
                "zero-x",
                base(),
                vec![Bus::slack("a", 1.0), Bus::load("b", 0.0, 0.0)],
                vec![Branch::new("l", 0, 1, 0.0, 0.0)],
            ),
            CaseError::ZeroReactance { branch: 0 },
        ),
    ];

    for (case, want) in bad {
        assert_eq!(solve_dc(&case), Err(PowerFlowError::Case(want)));
    }
}

/// A network in two pieces, only one of which has the slack. The disconnected
/// half has no angle reference, so the reduced matrix is singular.
#[test]
fn a_disconnected_island_is_reported_as_a_linear_failure() {
    let case = Case::new(
        "islanded",
        base(),
        vec![
            Bus::slack("s", 1.0),
            Bus::load("a", -0.5, 0.0),
            // Island: connected to nothing.
            Bus::load("x", -0.2, 0.0),
            Bus::load("y", 0.2, 0.0),
        ],
        vec![
            Branch::new("s-a", 0, 1, 0.0, 0.1),
            Branch::new("x-y", 2, 3, 0.0, 0.1),
        ],
    );

    let err = solve_dc(&case).unwrap_err();
    assert!(
        matches!(err, PowerFlowError::Linear(_)),
        "expected a linear failure, got {err:?}"
    );

    // And the message points at the network rather than the arithmetic, which
    // is where the reader needs to look.
    assert!(
        err.to_string().contains("disconnected"),
        "message should name the likely cause: {err}"
    );
}

/// A four-bus mesh with a loop, used by the invariant tests. Meshed rather
/// than radial because a radial network hides sign and loop-flow mistakes.
fn mesh_case() -> Case {
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
            Branch::new("s-a", 0, 1, 0.01, 0.10).with_rating(1.0),
            Branch::new("a-b", 1, 2, 0.01, 0.15).with_rating(1.0),
            Branch::new("b-g", 2, 3, 0.01, 0.12).with_rating(1.0),
            Branch::new("g-s", 3, 0, 0.01, 0.08).with_rating(1.0),
            Branch::new("a-g", 1, 3, 0.01, 0.20).with_rating(1.0),
        ],
    )
}
