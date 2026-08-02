//! Line outage distribution factors.
//!
//! `screen_agrees_with_a_full_resolve` is the test the two-tier N-1 design
//! stands or falls on. The screen exists because it is cheap; it is only
//! worth having because it is *exact* for the DC model, and the only way to
//! know that is to re-solve the network with the branch actually removed and
//! compare. No reference tool is involved — this is one path through the
//! repository checked against another.
//!
//! The exact-zero and exact-minus-one comparisons are deliberate. Those values
//! are written rather than computed, precisely so that an open branch carries
//! nothing instead of 1e-16, and a tolerance would stop checking that.
#![allow(clippy::float_cmp)]

use bs_case::{Branch, Bus, Case};
use bs_factors::{FactorError, Lodf, Ptdf, Screenability};
use bs_powerflow::solve_dc;
use bs_quantity::Base;

fn base() -> Base {
    Base::new(100.0, 138.0).unwrap()
}

fn close(got: f64, want: f64, tol: f64) -> bool {
    (got - want).abs() <= tol * want.abs().max(1.0)
}

/// A four-bus mesh: every branch outage leaves the network connected, so every
/// one is screenable and the comparison against a re-solve is meaningful for
/// all five.
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

/// The whole justification for screening rather than simulating. For every
/// branch in turn: predict the post-outage flows from the factors, then remove
/// the branch and solve properly, and require the two to agree.
#[test]
fn screen_agrees_with_a_full_resolve() {
    let case = mesh();

    let pre = solve_dc(&case).unwrap();
    let lodf = Lodf::build(&case).unwrap();

    for outaged in 0..case.n_branch() {
        assert!(
            lodf.is_screenable(outaged),
            "precondition: branch {outaged} should be screenable in a mesh"
        );

        let screened = lodf.outage(outaged, &pre.flows).unwrap();

        let mut opened = case.clone();
        opened.branches[outaged].in_service = false;
        let resolved = solve_dc(&opened).unwrap();

        for (monitored, (&predicted, &actual)) in screened.iter().zip(&resolved.flows).enumerate() {
            assert!(
                close(predicted, actual, 1e-9),
                "outage of {outaged}, branch {monitored}: screen says {predicted}, \
                 the re-solve says {actual}"
            );
        }
    }
}

/// After its own outage a branch carries nothing, so it loses precisely its
/// pre-outage flow. Written rather than computed: the general formula gives −1
/// only up to rounding, and a screen that leaves 1e-16 on an open branch
/// invites a violation report on a branch that is not there.
#[test]
fn a_branch_loses_exactly_its_own_flow() {
    let case = mesh();
    let lodf = Lodf::build(&case).unwrap();

    for outaged in 0..case.n_branch() {
        assert_eq!(
            lodf.get(outaged, outaged),
            -1.0,
            "branch {outaged} self-factor"
        );
    }

    // And the screened result puts exactly zero there, not nearly zero.
    let pre = solve_dc(&case).unwrap();
    for outaged in 0..case.n_branch() {
        assert_eq!(lodf.outage(outaged, &pre.flows).unwrap()[outaged], 0.0);
    }
}

/// Power still balances after the outage. The redistributed flows must satisfy
/// the same nodal balance the pre-outage flows did, which is a property of the
/// network rather than of the factor formula — so it fails if the rank-one
/// update is wrong in a way that happens to preserve the total.
#[test]
fn the_post_outage_flows_still_balance_at_every_bus() {
    let case = mesh();
    let pre = solve_dc(&case).unwrap();
    let lodf = Lodf::build(&case).unwrap();

    for outaged in 0..case.n_branch() {
        let flows = lodf.outage(outaged, &pre.flows).unwrap();
        let mut net = vec![0.0; case.n_bus()];

        for (i, branch) in case.branches.iter().enumerate() {
            net[branch.from] += flows[i];
            net[branch.to] -= flows[i];
        }

        let slack = case.slack_index().unwrap();

        for (bus, &exported) in net.iter().enumerate() {
            if bus == slack {
                continue;
            }
            assert!(
                close(exported, case.buses[bus].p_injection, 1e-9),
                "outage of {outaged}, bus {bus}: exports {exported}, injects {}",
                case.buses[bus].p_injection
            );
        }
    }
}

/// A radial branch is the only path between its ends, so its flow has nowhere
/// to redistribute. The factor formula divides by zero there. Reporting it as
/// a named condition rather than returning an enormous number is the whole
/// point: a screening result that is silently wrong is worse than one that is
/// openly missing.
#[test]
fn a_radial_branch_is_named_not_approximated() {
    // s — a — b, with a loop between s and a. Branch 2 (a—b) is the only path
    // to b, so its outage islands b.
    let case = Case::new(
        "radial-tail",
        base(),
        vec![
            Bus::slack("s", 1.0),
            Bus::load("a", -0.2, 0.0),
            Bus::load("b", -0.5, 0.0),
        ],
        vec![
            Branch::new("s-a", 0, 1, 0.0, 0.1),
            Branch::new("s-a-2", 0, 1, 0.0, 0.2),
            Branch::new("a-b", 1, 2, 0.0, 0.1),
        ],
    );

    let lodf = Lodf::build(&case).unwrap();

    assert_eq!(lodf.screenability(2), Screenability::Radial);
    assert!(
        lodf.is_screenable(0) && lodf.is_screenable(1),
        "the loop is fine"
    );

    let pre = solve_dc(&case).unwrap();
    assert_eq!(
        lodf.outage(2, &pre.flows),
        Err(FactorError::Radial { branch: 2 })
    );

    // The message has to say what to do about it, since a caller that cannot
    // screen this contingency still has to handle it.
    let msg = FactorError::Radial { branch: 2 }.to_string();
    assert!(msg.contains("radial") && msg.contains("Simulate"), "{msg}");
}

/// An already-open branch is not a contingency, and must not be reported as a
/// radial one — the two call for completely different responses.
#[test]
fn an_open_branch_is_not_a_contingency() {
    let mut case = mesh();
    case.branches[1].in_service = false;

    let lodf = Lodf::build(&case).unwrap();
    let pre = solve_dc(&case).unwrap();

    assert_eq!(lodf.screenability(1), Screenability::OutOfService);
    assert_eq!(
        lodf.outage(1, &pre.flows),
        Err(FactorError::OutOfService { branch: 1 })
    );

    // The remaining branches still screen correctly with one already out.
    let screened = lodf.outage(0, &pre.flows).unwrap();

    let mut opened = case.clone();
    opened.branches[0].in_service = false;
    let resolved = solve_dc(&opened).unwrap();

    for (monitored, (&predicted, &actual)) in screened.iter().zip(&resolved.flows).enumerate() {
        assert!(
            close(predicted, actual, 1e-9),
            "branch {monitored}: {predicted} vs {actual}"
        );
    }
}

/// Building from an existing PTDF must give the same matrix as building from
/// the case, or the optimization is a second implementation.
#[test]
fn building_from_a_ptdf_gives_the_same_matrix() {
    let case = mesh();
    let ptdf = Ptdf::build(&case).unwrap();

    assert_eq!(
        Lodf::build(&case).unwrap(),
        Lodf::from_ptdf(&case, &ptdf).unwrap()
    );
}

#[test]
fn repeated_builds_are_bit_identical() {
    let case = mesh();
    let first = Lodf::build(&case).unwrap();

    for _ in 0..10 {
        assert_eq!(first, Lodf::build(&case).unwrap());
    }
}
