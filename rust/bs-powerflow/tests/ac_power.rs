//! Bus power injections and the mismatch they leave.
//!
//! The one exact comparison is on an empty mismatch, which must be exactly
//! zero rather than nearly zero: nothing was summed, so anything else would
//! mean the fold started somewhere other than the identity.
#![allow(clippy::float_cmp)]
//!
//! The injection equations are what the whole AC solve is built from, so they
//! are checked two ways: against a hand-computable case, and against the DC
//! solve on a network where the two must agree. The second is the interesting
//! one — DC is a linearization of exactly these equations, so on a lossless
//! network at small angles it is not an approximation but the same answer,
//! computed along a completely different path.

use bs_case::{Branch, Bus, Case};
use bs_powerflow::ac::{Layout, Voltages, injections, max_mismatch, mismatch};
use bs_powerflow::{Ybus, solve_dc};
use bs_quantity::Base;

fn base() -> Base {
    Base::new(100.0, 138.0).unwrap()
}

fn close(got: f64, want: f64, tol: f64) -> bool {
    (got - want).abs() <= tol * want.abs().max(1.0)
}

/// Slack at 0, PQ at 1 and 2, PV at 3.
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
        ],
    )
}

/// Two buses, one branch with r = 0.03, x = 0.04, so y = 12 − j16. At flat
/// voltages both angles are zero, so every sine vanishes and each injection
/// reduces to |V|² times the row sum of G — which is zero, because a network
/// with no shunts draws nothing when every bus is at the same voltage.
#[test]
fn a_flat_state_on_a_shuntless_network_injects_nothing() {
    let case = Case::new(
        "two-bus",
        base(),
        vec![Bus::slack("s", 1.0), Bus::load("l", -0.5, -0.1)],
        vec![Branch::new("line", 0, 1, 0.03, 0.04)],
    );

    let power = injections(&Ybus::build(&case).unwrap(), &Voltages::flat(&case));

    for bus in 0..case.n_bus() {
        assert!(close(power.p[bus], 0.0, 1e-12), "P{bus} = {}", power.p[bus]);
        assert!(close(power.q[bus], 0.0, 1e-12), "Q{bus} = {}", power.q[bus]);
    }
}

/// Push one bus's angle by a known amount and check the injection against the
/// equation evaluated by hand.
///
/// With y = 12 − j16, θ₀ = 0, θ₁ = −0.05, and both magnitudes 1:
///   P₀ = G₀₀ + G₀₁cos(0.05) + B₀₁sin(0.05)
///      = 12 − 12cos(0.05) + 16sin(0.05)
#[test]
fn an_angle_difference_drives_power_by_the_equation() {
    let case = Case::new(
        "two-bus",
        base(),
        vec![Bus::slack("s", 1.0), Bus::load("l", -0.5, 0.0)],
        vec![Branch::new("line", 0, 1, 0.03, 0.04)],
    );

    let mut volts = Voltages::flat(&case);
    volts.angle[1] = -0.05;

    let power = injections(&Ybus::build(&case).unwrap(), &volts);

    let want = 12.0 - 12.0 * 0.05_f64.cos() + 16.0 * 0.05_f64.sin();
    assert!(
        close(power.p[0], want, 1e-12),
        "P0 = {}, want {want}",
        power.p[0]
    );

    // Whatever leaves bus 0 arrives at bus 1 less the losses, so the two are
    // not equal and opposite — that difference *is* the loss, and it must be
    // positive on a resistive line.
    let loss = power.p[0] + power.p[1];
    assert!(loss > 0.0, "losses came out {loss}, which is generation");
}

/// DC power flow is the linearization of these equations. On a lossless
/// network held at flat magnitudes with small angles, the two are not
/// approximately equal — they are the same quantity computed along completely
/// different paths, one through Ybus and trigonometry, the other through a
/// reduced susceptance matrix and one linear solve.
///
/// This is the strongest oracle-independent check available on the injection
/// equations, because a sign error or a swapped G and B survives every
/// structural test and dies here.
#[test]
fn injections_agree_with_the_dc_solve_in_the_linear_limit() {
    // Lossless: r = 0 everywhere, so DC's dropped loss term is genuinely zero
    // rather than merely small. Loads are light to keep angles small.
    let case = Case::new(
        "lossless",
        base(),
        vec![
            Bus::slack("s", 1.0),
            Bus::load("a", -0.05, 0.0),
            Bus::load("b", -0.08, 0.0),
            Bus::load("g", 0.04, 0.0),
        ],
        vec![
            Branch::new("s-a", 0, 1, 0.0, 0.10),
            Branch::new("a-b", 1, 2, 0.0, 0.15),
            Branch::new("b-g", 2, 3, 0.0, 0.12),
            Branch::new("g-s", 3, 0, 0.0, 0.08),
        ],
    );

    let dc = solve_dc(&case).unwrap();

    // Evaluate the AC injection equations at the DC answer's angles, with
    // magnitudes held flat as DC assumes.
    let mut volts = Voltages::flat(&case);
    volts.angle.clone_from(&dc.angles);

    let ac = injections(&Ybus::build(&case).unwrap(), &volts);

    let slack = case.slack_index().unwrap();

    for bus in 0..case.n_bus() {
        // The slack's injection is not scheduled — it is whatever the network
        // requires — so it is checked against that requirement below rather
        // than against the zero its constructor left.
        if bus == slack {
            continue;
        }

        assert!(
            close(ac.p[bus], case.buses[bus].p_injection, 2e-3),
            "bus {bus}: the AC equations say {} is injected where DC scheduled {}",
            ac.p[bus],
            case.buses[bus].p_injection
        );
    }

    // And the slack picks up exactly the rest. On a lossless network that is
    // the whole imbalance, which is the same number DC reports — arrived at
    // here through Ybus and trigonometry instead of a linear solve.
    let drawn: f64 = case.buses.iter().map(|b| b.p_injection).sum();
    assert!(
        close(ac.p[slack], -drawn, 1e-5),
        "the slack supplies {} but the network draws {}",
        ac.p[slack],
        -drawn
    );
    assert!(
        close(ac.p[slack], dc.slack_injection, 1e-5),
        "AC says the slack supplies {}, DC says {}",
        ac.p[slack],
        dc.slack_injection
    );
}

/// The mismatch vector must be ordered exactly like the state vector, or the
/// Jacobian maps one onto the other incorrectly and Newton converges to
/// something that is not the answer.
#[test]
fn the_mismatch_is_ordered_like_the_state() {
    // The generator is held at 1.0 rather than mixed()'s 1.02 so that the flat
    // start really is uniform. With a PV bus scheduled off nominal, flat is
    // not a zero-injection state and the residuals stop being readable
    // directly off the scheduled values — which is the property this test uses
    // to check the ordering.
    let mut case = mixed();
    case.buses[3] = Bus::generator("g", 0.3, 1.0);

    let layout = Layout::build(&case).unwrap();

    let power = injections(&Ybus::build(&case).unwrap(), &Voltages::flat(&case));
    let residual = mismatch(&case, &layout, &power);

    assert_eq!(residual.len(), layout.len());

    // At flat start on a shuntless network nothing is injected, so every
    // residual is exactly the scheduled value — which makes the ordering
    // readable directly off the result.
    for (row, &bus) in layout.angle_buses.iter().enumerate() {
        assert!(
            close(residual[row], case.buses[bus].p_injection, 1e-9),
            "row {row} should carry bus {bus}'s real-power residual"
        );
    }

    for (i, &bus) in layout.magnitude_buses.iter().enumerate() {
        let row = layout.n_angle() + i;
        assert!(
            close(residual[row], case.buses[bus].q_injection, 1e-9),
            "row {row} should carry bus {bus}'s reactive residual"
        );
    }
}

/// The infinity norm, not a Euclidean one: convergence has to mean every bus
/// balances, and a two-norm lets one badly unbalanced bus hide behind a
/// thousand converged ones as the network grows.
#[test]
fn convergence_is_judged_on_the_worst_bus() {
    assert!(close(max_mismatch(&[0.001, -0.5, 0.002]), 0.5, 1e-15));
    assert_eq!(max_mismatch(&[]), 0.0);

    // A thousand tiny residuals must not outvote one large one.
    let mut many = vec![1e-9; 1000];
    many.push(0.3);
    assert!(close(max_mismatch(&many), 0.3, 1e-15));
}
