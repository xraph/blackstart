//! Physical conservation laws the AC solution must satisfy.
//!
//! These link quantities the solver computed by different routes through a law
//! that must hold whatever it did internally. Losses appear once in the bus
//! injections and again, independently, in the branch currents; DC power flow
//! is the linearization of the very equations the AC solve satisfies. Neither
//! check consults a reference tool, so neither can be defeated by a golden
//! file regenerated from the code it was meant to check.

use bs_case::{Branch, Bus, Case};
use bs_powerflow::ac::solve_ac;
use bs_powerflow::solve_dc;
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

/// Net real power injected across all buses is, by definition, what the network
/// dissipates. That same loss must equal the I²R in the branches, computed from
/// the branch currents — a separate calculation sharing no code with the bus
/// injections beyond the voltages both read.
///
/// This is the strongest physical check available without an oracle. A solver
/// can converge neatly to the wrong point; it cannot do so while two
/// independently computed forms of the same conserved quantity agree.
#[test]
fn losses_equal_the_i_squared_r_in_the_branches() {
    let case = mixed();
    let solution = solve_ac(&case).unwrap();
    let volts = &solution.voltages;

    let from_injections: f64 = solution.injections.p.iter().sum();

    let mut from_branches = 0.0;
    for branch in &case.branches {
        if !branch.in_service {
            continue;
        }

        let (mag_f, ang_f) = (volts.magnitude[branch.from], volts.angle[branch.from]);
        let (mag_t, ang_t) = (volts.magnitude[branch.to], volts.angle[branch.to]);

        // |V_f − V_t|² in rectangular form.
        let dre = mag_f * ang_f.cos() - mag_t * ang_t.cos();
        let dim = mag_f * ang_f.sin() - mag_t * ang_t.sin();
        let drop_squared = dre.mul_add(dre, dim * dim);

        // |I|² = |ΔV|² / |z|², and the dissipation is |I|² r.
        let z_squared = branch.r.mul_add(branch.r, branch.x * branch.x);
        from_branches += drop_squared / z_squared * branch.r;
    }

    assert!(
        from_injections > 0.0,
        "the network came out generating {from_injections}, not losing"
    );
    assert!(
        close(from_injections, from_branches, 1e-7),
        "injections say the network loses {from_injections}, the branch \
         currents say {from_branches}"
    );
}

/// Losses must vanish without resistance and grow with it. A solver reporting
/// some plausible constant would pass the balance check above — both sides read
/// the same voltages — but not this one.
#[test]
fn losses_track_resistance() {
    let mut lossless = mixed();
    for branch in &mut lossless.branches {
        branch.r = 0.0;
    }

    let none: f64 = solve_ac(&lossless).unwrap().injections.p.iter().sum();
    assert!(
        none.abs() < 1e-9,
        "a network with no resistance lost {none}"
    );

    let base_loss: f64 = solve_ac(&mixed()).unwrap().injections.p.iter().sum();

    let mut heavier = mixed();
    for branch in &mut heavier.branches {
        branch.r *= 2.0;
    }
    let heavier_loss: f64 = solve_ac(&heavier).unwrap().injections.p.iter().sum();

    assert!(
        heavier_loss > base_loss,
        "doubling resistance moved losses from {base_loss} to {heavier_loss}"
    );
}

/// On a lossless network at light load the AC and DC answers must agree. DC is
/// the linearization of exactly the equations the AC solve satisfies, so this
/// compares two entirely separate code paths — one iterating over Ybus and
/// trigonometry, the other a single linear solve on a reduced susceptance
/// matrix — against each other.
#[test]
fn ac_reduces_to_dc_in_the_linear_limit() {
    let case = lossless_ring();

    let ac = solve_ac(&case).unwrap();
    let dc = solve_dc(&case).unwrap();

    for bus in 0..case.n_bus() {
        assert!(
            close(ac.voltages.angle[bus], dc.angles[bus], 5e-3),
            "bus {bus}: AC angle {} vs DC angle {}",
            ac.voltages.angle[bus],
            dc.angles[bus]
        );
    }

    for branch in 0..case.n_branch() {
        assert!(
            close(ac.flows[branch], dc.flows[branch], 5e-3),
            "branch {branch}: AC flow {} vs DC flow {}",
            ac.flows[branch],
            dc.flows[branch]
        );
    }
}

/// The two solvers must also agree on what the slack supplies, which is the
/// single number most likely to be compared across tools.
#[test]
fn the_two_solvers_agree_on_what_the_slack_supplies() {
    let case = lossless_ring();

    let ac = solve_ac(&case).unwrap();
    let dc = solve_dc(&case).unwrap();
    let slack = case.slack_index().unwrap();

    assert!(
        close(ac.injections.p[slack], dc.slack_injection, 1e-3),
        "AC says the slack supplies {}, DC says {}",
        ac.injections.p[slack],
        dc.slack_injection
    );
}

/// A lossless ring at light load: the regime where DC is not an approximation
/// of the AC equations but the same answer.
fn lossless_ring() -> Case {
    Case::new(
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
    )
}
