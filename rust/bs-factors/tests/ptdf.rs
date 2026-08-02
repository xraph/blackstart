//! Power transfer distribution factors.
//!
//! Several assertions here compare against exact zero rather than a tolerance.
//! That is deliberate and specific: the slack column and a self-transfer are
//! zero *by construction* — never written, or subtracted from themselves — so
//! a value merely near zero would mean arithmetic reached somewhere it should
//! not have. An epsilon would hide exactly the bug worth catching.
#![allow(clippy::float_cmp)]
//!
//! The load-bearing test here is `ptdf_reproduces_the_solve`. Everything the
//! contingency screen does rests on the factors agreeing exactly with the
//! solve they were derived from, and that check needs no reference tool: it
//! compares two independent paths through this repository against each other.

use bs_case::{Branch, Bus, Case};
use bs_factors::{FactorError, Ptdf};
use bs_quantity::Base;

fn base() -> Base {
    Base::new(100.0, 138.0).unwrap()
}

fn close(got: f64, want: f64, tol: f64) -> bool {
    (got - want).abs() <= tol * want.abs().max(1.0)
}

/// Two buses, one branch. Injecting a unit at bus 1 and withdrawing it at the
/// slack sends exactly one unit across the line, in the 1→0 direction — so in
/// the branch's own 0→1 convention the factor is −1.
#[test]
fn two_bus_matches_hand_calculation() {
    let case = Case::new(
        "two-bus",
        base(),
        vec![Bus::slack("s", 1.0), Bus::load("l", -0.5, 0.0)],
        vec![Branch::new("line", 0, 1, 0.01, 0.1)],
    );

    let ptdf = Ptdf::build(&case).unwrap();

    assert!(
        close(ptdf.get(0, 1), -1.0, 1e-12),
        "PTDF[0][1] = {}, want -1",
        ptdf.get(0, 1)
    );

    // Reactance does not appear: on a single path every unit goes over it
    // regardless of impedance. A factor that moved with x here would mean the
    // normalization is wrong.
    let stiffer = Case::new(
        "stiffer",
        base(),
        vec![Bus::slack("s", 1.0), Bus::load("l", -0.5, 0.0)],
        vec![Branch::new("line", 0, 1, 0.01, 0.9)],
    );

    assert!(close(Ptdf::build(&stiffer).unwrap().get(0, 1), -1.0, 1e-12));
}

/// Parallel paths of reactance 0.1 and 0.2 carry two thirds and one third.
/// This is the case a wrong sign convention survives on a radial network.
#[test]
fn parallel_paths_split_by_inverse_reactance() {
    let case = Case::new(
        "loop",
        base(),
        vec![
            Bus::slack("s", 1.0),
            Bus::load("a", 0.0, 0.0),
            Bus::load("b", -0.9, 0.0),
        ],
        vec![
            Branch::new("s-b", 0, 2, 0.0, 0.1),
            Branch::new("s-a", 0, 1, 0.0, 0.1),
            Branch::new("a-b", 1, 2, 0.0, 0.1),
        ],
    );

    let ptdf = Ptdf::build(&case).unwrap();

    assert!(
        close(ptdf.get(0, 2), -2.0 / 3.0, 1e-12),
        "{}",
        ptdf.get(0, 2)
    );
    assert!(
        close(ptdf.get(1, 2), -1.0 / 3.0, 1e-12),
        "{}",
        ptdf.get(1, 2)
    );
    assert!(
        close(ptdf.get(2, 2), -1.0 / 3.0, 1e-12),
        "{}",
        ptdf.get(2, 2)
    );
}

#[test]
fn an_unsolvable_case_is_rejected_as_a_case_error() {
    let case = Case::new(
        "no-slack",
        base(),
        vec![Bus::load("a", -0.5, 0.0), Bus::load("b", 0.5, 0.0)],
        vec![Branch::new("l", 0, 1, 0.0, 0.1)],
    );

    assert!(matches!(
        Ptdf::build(&case),
        Err(FactorError::Case(bs_case::CaseError::NoSlack))
    ));
}
