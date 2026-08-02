//! The Newton-Raphson Jacobian.
//!
//! A Jacobian is a matrix of derivatives, and there is a way to check
//! derivatives that needs no reference tool and no hand algebra: perturb each
//! unknown, re-evaluate the function, and compare the difference quotient
//! against the analytic entry. If they agree at every position, the analytic
//! form is right — and if a single sign or a single factor of |V| is wrong,
//! exactly the affected entries disagree and the failure message names them.
//!
//! That is the whole test file. Everything else here is structure: the right
//! dimension, the right blocks present, symmetry where the physics requires
//! it. A wrong Jacobian does not make Newton fail, it makes it converge
//! slowly, or to a different point, or not at all on a hard case — none of
//! which is obvious from a passing solve on an easy one.

use bs_case::{Branch, Bus, Case};
use bs_powerflow::Ybus;
use bs_powerflow::ac::{Injections, Layout, Voltages, injections, jacobian};
use bs_quantity::Base;

fn base() -> Base {
    Base::new(100.0, 138.0).unwrap()
}

/// Slack, two PQ buses and a PV bus, meshed. All four Jacobian blocks are
/// non-empty on this case, which a radial or all-PQ network would not give.
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
            Branch::new("s-a", 0, 1, 0.02, 0.10).with_charging(0.03),
            Branch::new("a-b", 1, 2, 0.03, 0.15).with_charging(0.04),
            Branch::new("b-g", 2, 3, 0.02, 0.12),
            Branch::new("g-s", 3, 0, 0.01, 0.08).with_charging(0.02),
            Branch::new("a-g", 1, 3, 0.04, 0.20),
        ],
    )
}

/// A state away from flat, so that every trigonometric term is exercised
/// rather than collapsing to sin 0 = 0.
fn perturbed(case: &Case) -> Voltages {
    let mut volts = Voltages::flat(case);
    volts.angle = vec![0.0, -0.06, -0.11, -0.03];
    volts.magnitude = vec![1.0, 0.98, 0.96, 1.02];
    volts
}

/// The residual vector the Jacobian differentiates, in layout order.
///
/// Deliberately not `mismatch`: the scheduled values are constants and would
/// cancel from every difference quotient, so differencing the computed
/// injections directly tests the same derivative with one less thing to get
/// wrong in the test itself.
fn computed(ybus: &Ybus, layout: &Layout, volts: &Voltages) -> Vec<f64> {
    let power: Injections = injections(ybus, volts);

    let mut out = Vec::with_capacity(layout.len());
    out.extend(layout.angle_buses.iter().map(|&bus| power.p[bus]));
    out.extend(layout.magnitude_buses.iter().map(|&bus| power.q[bus]));
    out
}

/// Perturbs unknown `col` by `h`, in the layout's own ordering.
fn nudge(layout: &Layout, volts: &Voltages, col: usize, h: f64) -> Voltages {
    let mut moved = volts.clone();
    let mut step = vec![0.0; layout.len()];
    step[col] = h;
    layout.apply(&step, &mut moved.angle, &mut moved.magnitude);
    moved
}

/// The check that matters. Every analytic entry against a central difference
/// of the function it claims to differentiate.
///
/// Central rather than forward: its error is O(h²) instead of O(h), which buys
/// enough accuracy at h = 1e-6 to distinguish a correct entry from one off by
/// a factor of |V| — around 0.96 here, which a sloppier comparison would wave
/// through.
///
/// The sign convention is what this pins down, and it is worth stating
/// exactly. The matrix built here is the Jacobian of the *computed
/// injections* — that is what the difference quotient below measures, and the
/// test requires equality, not a sign flip. The mismatch is
/// `scheduled − computed`, so the residual Newton drives to zero has Jacobian
/// `−J`, and the Newton step `J_f Δx = −f` becomes `J Δx = mismatch` with no
/// negation anywhere. One place in the solver has to get that right, and this
/// is the test that says which convention it settled on.
#[test]
fn every_entry_matches_a_finite_difference() {
    let case = mixed();
    let ybus = Ybus::build(&case).unwrap();
    let layout = Layout::build(&case).unwrap();
    let volts = perturbed(&case);

    let power = injections(&ybus, &volts);
    let analytic = jacobian::build(&ybus, &layout, &volts, &power).unwrap();

    let h = 1e-6;
    let mut worst = (0.0_f64, 0, 0);

    for col in 0..layout.len() {
        let plus = computed(&ybus, &layout, &nudge(&layout, &volts, col, h));
        let minus = computed(&ybus, &layout, &nudge(&layout, &volts, col, -h));

        for row in 0..layout.len() {
            let numeric = (plus[row] - minus[row]) / (2.0 * h);
            let error = (analytic.get(row, col) - numeric).abs();

            if error > worst.0 {
                worst = (error, row, col);
            }

            assert!(
                error <= 1e-5 * numeric.abs().max(1.0),
                "J({row},{col}) = {}, but differencing gives {numeric}",
                analytic.get(row, col)
            );
        }
    }

    // A passing tolerance is not the same as a tight one. If the worst error
    // ever creeps toward the bound this test is measuring the tolerance rather
    // than the derivative, so the actual margin is asserted too.
    assert!(
        worst.0 < 1e-6,
        "worst disagreement {} at ({}, {}) is close enough to the tolerance \
         that this test has stopped proving much",
        worst.0,
        worst.1,
        worst.2
    );
}

/// The same check at a flat start, where every sine vanishes and several terms
/// drop out. A derivative that is right away from flat can still be wrong
/// exactly where half the expression disappears, and flat is where every solve
/// begins.
#[test]
fn the_derivatives_are_right_at_a_flat_start() {
    let case = mixed();
    let ybus = Ybus::build(&case).unwrap();
    let layout = Layout::build(&case).unwrap();
    let volts = Voltages::flat(&case);

    let power = injections(&ybus, &volts);
    let analytic = jacobian::build(&ybus, &layout, &volts, &power).unwrap();

    let h = 1e-6;

    for col in 0..layout.len() {
        let plus = computed(&ybus, &layout, &nudge(&layout, &volts, col, h));
        let minus = computed(&ybus, &layout, &nudge(&layout, &volts, col, -h));

        for row in 0..layout.len() {
            let numeric = (plus[row] - minus[row]) / (2.0 * h);
            assert!(
                (analytic.get(row, col) - numeric).abs() <= 1e-5 * numeric.abs().max(1.0),
                "at flat start J({row},{col}) = {}, differencing gives {numeric}",
                analytic.get(row, col)
            );
        }
    }
}

/// Square at the number of unknowns, with the slack and every PV magnitude
/// absent rather than pinned — the same way the DC reduction deletes the slack
/// row instead of constraining it.
#[test]
fn the_matrix_covers_exactly_the_unknowns() {
    let case = mixed();
    let ybus = Ybus::build(&case).unwrap();
    let layout = Layout::build(&case).unwrap();
    let volts = perturbed(&case);
    let power = injections(&ybus, &volts);

    let j = jacobian::build(&ybus, &layout, &volts, &power).unwrap();

    // 3 angles (buses 1, 2, 3) + 2 magnitudes (buses 1, 2).
    assert_eq!(layout.len(), 5);
    assert_eq!(j.rows(), 5);
    assert_eq!(j.cols(), 5);
}

/// Every unknown must appear in some equation, or the matrix is singular and
/// Newton has nothing to solve. An all-zero row or column is the signature of
/// a block that was never emitted, which a dimension check alone would miss.
#[test]
fn no_row_or_column_is_empty() {
    let case = mixed();
    let ybus = Ybus::build(&case).unwrap();
    let layout = Layout::build(&case).unwrap();
    let volts = perturbed(&case);
    let power = injections(&ybus, &volts);

    let j = jacobian::build(&ybus, &layout, &volts, &power).unwrap();

    for row in 0..j.rows() {
        let sum: f64 = j.row(row).map(|(_, v)| v.abs()).sum();
        assert!(sum > 1e-12, "row {row} is empty; a block was never emitted");
    }

    for col in 0..j.cols() {
        let sum: f64 = (0..j.rows()).map(|r| j.get(r, col).abs()).sum();
        assert!(
            sum > 1e-12,
            "column {col} is empty; unknown {col} is unconstrained"
        );
    }
}

#[test]
fn repeated_builds_are_bit_identical() {
    let case = mixed();
    let ybus = Ybus::build(&case).unwrap();
    let layout = Layout::build(&case).unwrap();
    let volts = perturbed(&case);
    let power = injections(&ybus, &volts);

    let first = jacobian::build(&ybus, &layout, &volts, &power).unwrap();

    for _ in 0..10 {
        let again = jacobian::build(&ybus, &layout, &volts, &power).unwrap();
        assert_eq!(first.to_dense(), again.to_dense());
    }
}
