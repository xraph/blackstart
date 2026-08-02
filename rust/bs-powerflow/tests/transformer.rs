//! Transformers, phase shifters and bus shunts.
//!
//! These exist because real cases have them — every IEEE test network and
//! every MATPOWER case does — and because omitting them is the kind of gap
//! that produces plausible numbers rather than an error. A network solved
//! without its transformer ratios converges neatly to the wrong answer.
//!
//! The convention followed is MATPOWER's and pandapower's: the ratio applies
//! at the from-end, so an imported case needs no reinterpretation.

use bs_case::{Branch, Bus, Case, CaseError};
use bs_powerflow::ac::solve_ac;
use bs_powerflow::{Ybus, YbusError, solve_dc};
use bs_quantity::Base;

fn base() -> Base {
    Base::new(100.0, 138.0).unwrap()
}

fn close(got: f64, want: f64, tol: f64) -> bool {
    (got - want).abs() <= tol * want.abs().max(1.0)
}

fn two_bus(tap: f64, shift: f64) -> Case {
    Case::new(
        "two-bus",
        base(),
        vec![Bus::slack("s", 1.0), Bus::load("l", -0.5, -0.1)],
        vec![Branch::new("line", 0, 1, 0.03, 0.04).with_transformer(tap, shift)],
    )
}

/// A branch with a unit ratio and no shift must give exactly what a plain line
/// gave before transformers existed. Otherwise every network without a
/// transformer just changed answer.
#[test]
fn a_unit_ratio_is_indistinguishable_from_a_line() {
    let plain = Ybus::build(&two_bus(1.0, 0.0)).unwrap();

    // y = 1/(0.03 + j0.04) = 12 − j16, as the original hand calculation.
    assert!(close(plain.g.get(0, 0), 12.0, 1e-12));
    assert!(close(plain.b.get(0, 0), -16.0, 1e-12));
    assert!(close(plain.g.get(0, 1), -12.0, 1e-12));
    assert!(close(plain.b.get(0, 1), 16.0, 1e-12));
    assert!(close(plain.g.get(1, 1), 12.0, 1e-12));
}

/// The ratio scales the from-diagonal by 1/tap² and both off-diagonals by
/// 1/tap, leaving the to-diagonal alone. That asymmetry is the whole point of
/// a transformer and is easy to apply to the wrong end.
#[test]
fn a_turns_ratio_scales_the_from_end_only() {
    let tap = 1.25;
    let y = Ybus::build(&two_bus(tap, 0.0)).unwrap();

    assert!(
        close(y.g.get(0, 0), 12.0 / (tap * tap), 1e-12),
        "G00 = {}, want {}",
        y.g.get(0, 0),
        12.0 / (tap * tap)
    );
    assert!(
        close(y.g.get(0, 1), -12.0 / tap, 1e-12),
        "G01 = {}",
        y.g.get(0, 1)
    );
    assert!(
        close(y.g.get(1, 0), -12.0 / tap, 1e-12),
        "G10 = {}",
        y.g.get(1, 0)
    );
    assert!(
        close(y.g.get(1, 1), 12.0, 1e-12),
        "the to-end diagonal moved: {}",
        y.g.get(1, 1)
    );
}

/// A phase shifter makes Y legitimately asymmetric — `Y_ft` uses `conj(τ)` and
/// `Y_tf` uses `τ`. A symmetric result here would mean the shift was dropped.
#[test]
fn a_phase_shifter_makes_the_matrix_asymmetric() {
    let y = Ybus::build(&two_bus(1.0, 0.1)).unwrap();

    assert!(
        (y.g.get(0, 1) - y.g.get(1, 0)).abs() > 1e-6,
        "G01 = {} and G10 = {} are symmetric; the shift was dropped",
        y.g.get(0, 1),
        y.g.get(1, 0)
    );

    // The diagonals stay put: a shift moves no magnitude.
    let plain = Ybus::build(&two_bus(1.0, 0.0)).unwrap();
    assert!(close(y.g.get(0, 0), plain.g.get(0, 0), 1e-12));
    assert!(close(y.g.get(1, 1), plain.g.get(1, 1), 1e-12));
}

/// A shunt is a property of the bus and lands on its diagonal — not as a
/// branch to a fictitious ground bus, which would corrupt the index space
/// every result is reported in.
#[test]
fn a_bus_shunt_lands_on_the_diagonal() {
    let mut case = two_bus(1.0, 0.0);
    case.buses[1] = Bus::load("l", -0.5, -0.1).with_shunt(0.02, 0.3);

    let y = Ybus::build(&case).unwrap();
    let plain = Ybus::build(&two_bus(1.0, 0.0)).unwrap();

    assert!(close(y.g.get(1, 1) - plain.g.get(1, 1), 0.02, 1e-12));
    assert!(close(y.b.get(1, 1) - plain.b.get(1, 1), 0.3, 1e-12));

    // Nowhere else.
    assert!(close(y.g.get(0, 1), plain.g.get(0, 1), 1e-12));
    assert!(close(y.g.get(0, 0), plain.g.get(0, 0), 1e-12));
    assert_eq!(y.g.rows(), 2, "a shunt added a bus");
}

/// The DC model divides the susceptance by the tap, so a higher ratio carries
/// less flow for the same angle difference.
#[test]
fn the_dc_flow_responds_to_the_turns_ratio() {
    let mut plain = two_bus(1.0, 0.0);
    plain.branches[0].x = 0.1;
    plain.branches[0].r = 0.0;

    let mut tapped = plain.clone();
    tapped.branches[0].tap = 2.0;

    let a = solve_dc(&plain).unwrap();
    let b = solve_dc(&tapped).unwrap();

    // Flow is fixed by the load, so the angle difference must double.
    assert!(close(a.flows[0], 0.5, 1e-12));
    assert!(close(b.flows[0], 0.5, 1e-12));
    assert!(
        close(b.angles[1], 2.0 * a.angles[1], 1e-12),
        "angle went from {} to {} — the tap did not reach the reduction",
        a.angles[1],
        b.angles[1]
    );
}

/// A phase shifter forces power across itself independently of the angle
/// difference. Dropping it does not fail — it silently reports the flow the
/// network would have had without the shifter, which is the worst kind of
/// quiet.
#[test]
fn a_phase_shifter_carries_flow_the_angles_alone_would_not() {
    // A loop, so the shifter has somewhere to push power to.
    let mut case = Case::new(
        "loop",
        base(),
        vec![
            Bus::slack("s", 1.0),
            Bus::load("a", 0.0, 0.0),
            Bus::load("b", -0.6, 0.0),
        ],
        vec![
            Branch::new("s-b", 0, 2, 0.0, 0.1),
            Branch::new("s-a", 0, 1, 0.0, 0.1),
            Branch::new("a-b", 1, 2, 0.0, 0.1),
        ],
    );

    let without = solve_dc(&case).unwrap();

    case.branches[1].shift = 0.02;
    let with = solve_dc(&case).unwrap();

    assert!(
        (with.flows[1] - without.flows[1]).abs() > 1e-6,
        "the shifter changed nothing: {} vs {}",
        without.flows[1],
        with.flows[1]
    );

    // Power still balances: the shifter redirects flow, it does not create it.
    let drawn: f64 = case.buses.iter().map(|b| b.p_injection).sum();
    assert!(close(with.slack_injection, -drawn, 1e-12));
}

/// The cross-check that matters. With a transformer in the network, AC and DC
/// must still agree in the lossless small-angle limit — which they only can if
/// both models apply the ratio the same way. Two independent implementations,
/// one property.
#[test]
fn ac_and_dc_agree_with_a_transformer_present() {
    let case = Case::new(
        "transformed",
        base(),
        vec![
            Bus::slack("s", 1.0),
            Bus::load("a", -0.05, 0.0),
            Bus::load("b", -0.06, 0.0),
        ],
        vec![
            Branch::new("s-a", 0, 1, 0.0, 0.10).with_transformer(1.0, 0.0),
            Branch::new("a-b", 1, 2, 0.0, 0.12).with_transformer(1.0, 0.0),
            Branch::new("b-s", 2, 0, 0.0, 0.15).with_transformer(1.0, 0.0),
        ],
    );

    let ac = solve_ac(&case).unwrap();
    let dc = solve_dc(&case).unwrap();

    for branch in 0..case.n_branch() {
        assert!(
            close(ac.flows[branch], dc.flows[branch], 5e-3),
            "branch {branch}: AC {} vs DC {}",
            ac.flows[branch],
            dc.flows[branch]
        );
    }
}

/// A zero or negative ratio has no physical meaning and would divide the
/// admittance by zero. Naming it beats a matrix full of infinities.
#[test]
fn a_non_positive_turns_ratio_is_rejected() {
    for bad in [0.0, -1.0] {
        let case = two_bus(bad, 0.0);
        assert!(
            matches!(
                Ybus::build(&case),
                Err(YbusError::Case(CaseError::BadTap { branch: 0, .. }))
            ),
            "a ratio of {bad} was accepted"
        );
    }

    // And the message says what a valid ratio looks like, since 1.0 is not
    // obviously the identity to someone reading a failure for the first time.
    let msg = CaseError::BadTap {
        branch: 3,
        tap: 0.0,
    }
    .to_string();
    assert!(msg.contains("1.0") && msg.contains("positive"), "{msg}");
}

#[test]
fn defaults_are_the_identity() {
    let branch = Branch::new("b", 0, 1, 0.01, 0.1);
    assert!(close(branch.tap, 1.0, 1e-15));
    assert!(close(branch.shift, 0.0, 1e-15));

    let bus = Bus::load("l", -0.5, 0.0);
    assert!(close(bus.g_shunt, 0.0, 1e-15));
    assert!(close(bus.b_shunt, 0.0, 1e-15));
}
