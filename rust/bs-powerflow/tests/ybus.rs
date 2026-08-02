//! Admittance matrix assembly.
//!
//! Ybus is where every AC result comes from, so a mistake here is invisible
//! everywhere and wrong everywhere. The checks are of two kinds: hand-computed
//! entries for a network small enough to verify with a pencil, and structural
//! properties — symmetry, zero row sums without shunts — that hold for any
//! network and would catch an error the small case happens to miss.

use bs_case::{Branch, Bus, Case};
use bs_powerflow::{Ybus, YbusError};
use bs_quantity::Base;

fn base() -> Base {
    Base::new(100.0, 138.0).unwrap()
}

fn close(got: f64, want: f64, tol: f64) -> bool {
    (got - want).abs() <= tol * want.abs().max(1.0)
}

fn mesh() -> Case {
    Case::new(
        "mesh",
        base(),
        vec![
            Bus::slack("s", 1.0),
            Bus::load("a", -0.4, -0.1),
            Bus::load("b", -0.7, -0.2),
            Bus::generator("g", 0.3, 1.0),
        ],
        vec![
            Branch::new("s-a", 0, 1, 0.02, 0.10).with_charging(0.03),
            Branch::new("a-b", 1, 2, 0.03, 0.15).with_charging(0.04),
            Branch::new("b-g", 2, 3, 0.02, 0.12),
            Branch::new("g-s", 3, 0, 0.01, 0.08).with_charging(0.02),
            Branch::new("a-g", 1, 3, 0.04, 0.20),
        ],
    )
}

/// One branch with r = 0.03, x = 0.04. Then r² + x² = 0.0025, so the series
/// admittance is 0.03/0.0025 − j0.04/0.0025 = 12 − j16.
///
/// Every other test here rests on this being right.
#[test]
fn a_single_branch_matches_hand_calculation() {
    let case = Case::new(
        "two-bus",
        base(),
        vec![Bus::slack("s", 1.0), Bus::load("l", -0.5, -0.1)],
        vec![Branch::new("line", 0, 1, 0.03, 0.04)],
    );

    let y = Ybus::build(&case).unwrap();

    assert!(close(y.g.get(0, 0), 12.0, 1e-12), "G00 = {}", y.g.get(0, 0));
    assert!(
        close(y.b.get(0, 0), -16.0, 1e-12),
        "B00 = {}",
        y.b.get(0, 0)
    );
    assert!(
        close(y.g.get(0, 1), -12.0, 1e-12),
        "G01 = {}",
        y.g.get(0, 1)
    );
    assert!(close(y.b.get(0, 1), 16.0, 1e-12), "B01 = {}", y.b.get(0, 1));

    // Both ends are symmetric for a plain line.
    assert!(close(y.g.get(1, 1), 12.0, 1e-12));
    assert!(close(y.b.get(1, 1), -16.0, 1e-12));
}

/// Line charging is a shunt to ground at each terminal, so half lands on each
/// diagonal and none on the off-diagonals. Getting that wrong shifts every
/// reactive result while leaving real power almost untouched — a failure mode
/// that a real-power-only check would sail straight past.
#[test]
fn charging_goes_to_the_diagonal_only() {
    let plain = Case::new(
        "plain",
        base(),
        vec![Bus::slack("s", 1.0), Bus::load("l", -0.5, 0.0)],
        vec![Branch::new("line", 0, 1, 0.03, 0.04)],
    );

    let charged = Case::new(
        "charged",
        base(),
        vec![Bus::slack("s", 1.0), Bus::load("l", -0.5, 0.0)],
        vec![Branch::new("line", 0, 1, 0.03, 0.04).with_charging(0.06)],
    );

    let (a, b) = (Ybus::build(&plain).unwrap(), Ybus::build(&charged).unwrap());

    // Half of 0.06 on each diagonal.
    assert!(close(b.b.get(0, 0) - a.b.get(0, 0), 0.03, 1e-12));
    assert!(close(b.b.get(1, 1) - a.b.get(1, 1), 0.03, 1e-12));

    // Nothing off-diagonal, and nothing at all in the conductance.
    assert!(close(b.b.get(0, 1), a.b.get(0, 1), 1e-12));
    assert!(close(b.g.get(0, 0), a.g.get(0, 0), 1e-12));
}

/// A passive network of two-terminal branches gives a symmetric Ybus. This is
/// a property of any such network, so it catches an assembly that adds a term
/// to one side of the matrix and not the other — which a small hand-checked
/// case can easily miss.
#[test]
fn the_matrix_is_symmetric() {
    let y = Ybus::build(&mesh()).unwrap();

    for i in 0..y.n_bus() {
        for j in 0..y.n_bus() {
            assert!(
                close(y.g.get(i, j), y.g.get(j, i), 1e-12),
                "G({i},{j}) = {} but G({j},{i}) = {}",
                y.g.get(i, j),
                y.g.get(j, i)
            );
            assert!(close(y.b.get(i, j), y.b.get(j, i), 1e-12), "B({i},{j})");
        }
    }
}

/// Without shunts, every row of Ybus sums to zero: a uniform voltage on every
/// bus drives no current anywhere. Charging susceptance is the one shunt in
/// this model, so the conductance rows sum to zero even when charging is
/// present, and the susceptance rows sum to exactly the charging at that bus.
///
/// This is the strongest structural check available without a reference tool,
/// because it constrains every entry in the row at once.
#[test]
fn rows_sum_to_their_shunt() {
    let case = mesh();
    let y = Ybus::build(&case).unwrap();

    for bus in 0..y.n_bus() {
        let g_sum: f64 = y.g.row(bus).map(|(_, v)| v).sum();
        assert!(
            close(g_sum, 0.0, 1e-10),
            "conductance row {bus} sums to {g_sum}, want 0 — there are no \
             conductive shunts in this model"
        );

        // Half of each incident branch's charging.
        let expected: f64 = case
            .branches
            .iter()
            .filter(|br| br.in_service && (br.from == bus || br.to == bus))
            .map(|br| br.b / 2.0)
            .sum();

        let b_sum: f64 = y.b.row(bus).map(|(_, v)| v).sum();
        assert!(
            close(b_sum, expected, 1e-10),
            "susceptance row {bus} sums to {b_sum}, want the incident charging {expected}"
        );
    }
}

#[test]
fn an_out_of_service_branch_contributes_nothing() {
    let case = mesh();
    let full = Ybus::build(&case).unwrap();

    let mut opened = case.clone();
    opened.branches[4].in_service = false;
    let reduced = Ybus::build(&opened).unwrap();

    // Branch 4 joins buses 1 and 3, so exactly those entries move.
    assert!(
        reduced.g.get(1, 3).abs() < 1e-15,
        "the off-diagonal survived"
    );
    assert!(reduced.b.get(1, 3).abs() < 1e-15);
    assert!(full.g.get(1, 3).abs() > 1e-6, "precondition: it was there");

    // Buses the branch never touched are untouched.
    assert!(close(reduced.g.get(0, 0), full.g.get(0, 0), 1e-12));
    assert!(close(reduced.g.get(2, 2), full.g.get(2, 2), 1e-12));
}

/// Parallel branches between the same pair of buses must add rather than
/// replace. The Triplets layer sums duplicate coordinates, and this is the
/// case where that behaviour is load-bearing rather than incidental.
#[test]
fn parallel_branches_add() {
    let single = Case::new(
        "single",
        base(),
        vec![Bus::slack("s", 1.0), Bus::load("l", -0.5, 0.0)],
        vec![Branch::new("a", 0, 1, 0.03, 0.04)],
    );

    let doubled = Case::new(
        "doubled",
        base(),
        vec![Bus::slack("s", 1.0), Bus::load("l", -0.5, 0.0)],
        vec![
            Branch::new("a", 0, 1, 0.03, 0.04),
            Branch::new("b", 0, 1, 0.03, 0.04),
        ],
    );

    let (one, two) = (
        Ybus::build(&single).unwrap(),
        Ybus::build(&doubled).unwrap(),
    );

    assert!(close(two.g.get(0, 0), 2.0 * one.g.get(0, 0), 1e-12));
    assert!(close(two.b.get(0, 1), 2.0 * one.b.get(0, 1), 1e-12));
}

#[test]
fn repeated_builds_are_bit_identical() {
    let case = mesh();
    let first = Ybus::build(&case).unwrap();

    for _ in 0..10 {
        let again = Ybus::build(&case).unwrap();
        assert_eq!(first.g.to_dense(), again.g.to_dense());
        assert_eq!(first.b.to_dense(), again.b.to_dense());
    }
}

#[test]
fn an_invalid_case_is_rejected_before_assembly() {
    let case = Case::new(
        "no-slack",
        base(),
        vec![Bus::load("a", -0.5, 0.0), Bus::load("b", 0.5, 0.0)],
        vec![Branch::new("l", 0, 1, 0.01, 0.1)],
    );

    assert!(matches!(Ybus::build(&case), Err(YbusError::Case(_))));
}
