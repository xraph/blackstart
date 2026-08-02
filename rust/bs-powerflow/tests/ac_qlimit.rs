//! Generator reactive limit enforcement.
//!
//! The failure this guards against is not a crash. Without limits, a stressed
//! network reports a comfortable voltage profile that no physical generator
//! could produce — a real finding silently converted into a clean answer,
//! which for a tool whose job is to find voltage problems is the worst
//! available outcome. So the tests check that a binding limit is detected,
//! that the resulting solution respects it, and that the voltage moves the way
//! physics says it must when reactive support runs out.
//!
//! The exact comparisons are against the infinities that mean "unlimited".
//! Those are sentinels, not measurements — a value merely near infinity does
//! not exist — so equality is the only assertion that says anything.
#![allow(clippy::float_cmp)]

use bs_case::{Branch, Bus, BusKind, Case};
use bs_powerflow::ac::{Enforcement, Options, solve_ac, solve_with_limits};
use bs_quantity::Base;

fn base() -> Base {
    Base::new(100.0, 138.0).unwrap()
}

fn close(got: f64, want: f64, tol: f64) -> bool {
    (got - want).abs() <= tol * want.abs().max(1.0)
}

/// A generator at bus 3 holding 1.05 against a heavy reactive load, so it must
/// produce real reactive power to do it. `q_max` decides whether it can.
fn stressed(q_max: f64) -> Case {
    Case::new(
        "stressed",
        base(),
        vec![
            Bus::slack("s", 1.0),
            Bus::load("a", -0.4, -0.30),
            Bus::load("b", -0.7, -0.55),
            Bus::generator("g", 0.3, 1.05).with_reactive_limits(-1.0, q_max),
        ],
        vec![
            Branch::new("s-a", 0, 1, 0.02, 0.10),
            Branch::new("a-b", 1, 2, 0.03, 0.15),
            Branch::new("b-g", 2, 3, 0.02, 0.12),
            Branch::new("g-s", 3, 0, 0.01, 0.08),
        ],
    )
}

/// A generator with room to spare stays on voltage control, holds its
/// schedule, and the answer matches the unlimited solve exactly. Enforcement
/// must cost nothing when nothing binds.
#[test]
fn a_generator_within_its_limits_is_untouched() {
    let case = stressed(10.0);
    let limited = solve_with_limits(&case, Options::default()).unwrap();

    assert_eq!(limited.enforcement[3], Enforcement::OnVoltage);
    assert!(limited.binding().is_empty());
    assert_eq!(limited.rounds, 0, "a case with no binding limit re-solved");

    assert!(close(limited.solution.voltages.magnitude[3], 1.05, 1e-9));

    // Same answer as solving without limits at all.
    let unlimited = solve_ac(&case).unwrap();
    for bus in 0..case.n_bus() {
        assert!(close(
            limited.solution.voltages.magnitude[bus],
            unlimited.voltages.magnitude[bus],
            1e-9
        ));
    }
}

/// The test that matters. A generator asked for more reactive power than it
/// has must end up producing exactly its limit, not more — and its voltage
/// must fall below schedule, because that is what running out of reactive
/// support does.
#[test]
fn a_binding_limit_fixes_the_output_and_lets_the_voltage_fall() {
    // First establish what the generator would produce unlimited, so the limit
    // below is known to bind rather than assumed to.
    let free = solve_ac(&stressed(10.0)).unwrap();
    let wanted = free.injections.q[3];
    assert!(
        wanted > 0.2,
        "precondition: the generator should want {wanted}"
    );

    let q_max = wanted / 2.0;
    let case = stressed(q_max);
    let limited = solve_with_limits(&case, Options::default()).unwrap();

    assert_eq!(limited.enforcement[3], Enforcement::AtMaximum);
    assert_eq!(limited.binding(), vec![3]);
    assert!(limited.rounds >= 1);

    // Producing exactly its limit, not what it wanted.
    assert!(
        close(limited.solution.injections.q[3], q_max, 1e-6),
        "generator produced {} against a limit of {q_max}",
        limited.solution.injections.q[3]
    );

    // And the voltage it could no longer hold has fallen.
    assert!(
        limited.solution.voltages.magnitude[3] < 1.05,
        "the generator hit its limit but still holds {}",
        limited.solution.voltages.magnitude[3]
    );
}

/// Tightening the limit further must depress the voltage further. A single
/// binding case could pass by accident; a monotone response cannot.
#[test]
fn a_tighter_limit_depresses_the_voltage_further() {
    let wanted = solve_ac(&stressed(10.0)).unwrap().injections.q[3];

    let loose = solve_with_limits(&stressed(wanted * 0.75), Options::default()).unwrap();
    let tight = solve_with_limits(&stressed(wanted * 0.25), Options::default()).unwrap();

    assert_eq!(loose.enforcement[3], Enforcement::AtMaximum);
    assert_eq!(tight.enforcement[3], Enforcement::AtMaximum);

    assert!(
        tight.solution.voltages.magnitude[3] < loose.solution.voltages.magnitude[3],
        "tightening the limit from {} to {} raised the voltage from {} to {}",
        wanted * 0.75,
        wanted * 0.25,
        loose.solution.voltages.magnitude[3],
        tight.solution.voltages.magnitude[3]
    );
}

/// The final state must be a genuine solution of the network it describes —
/// the one with the generator fixed at its limit. Enforcement must not leave
/// the answer converged for a different case than the one it reports.
#[test]
fn the_limited_solution_still_solves_its_own_network() {
    let wanted = solve_ac(&stressed(10.0)).unwrap().injections.q[3];
    let q_max = wanted / 2.0;

    let limited = solve_with_limits(&stressed(q_max), Options::default()).unwrap();

    // Rebuild the network the enforcement settled on and solve it directly.
    let mut settled = stressed(q_max);
    settled.buses[3].kind = BusKind::Pq;
    settled.buses[3].q_injection = q_max;
    settled.buses[3].v_magnitude = limited.solution.voltages.magnitude[3];

    let direct = solve_ac(&settled).unwrap();

    for bus in 0..settled.n_bus() {
        assert!(
            close(
                limited.solution.voltages.magnitude[bus],
                direct.voltages.magnitude[bus],
                1e-6
            ),
            "bus {bus}: enforcement gave {}, solving the settled network gives {}",
            limited.solution.voltages.magnitude[bus],
            direct.voltages.magnitude[bus]
        );
        assert!(close(
            limited.solution.voltages.angle[bus],
            direct.voltages.angle[bus],
            1e-6
        ));
    }

    assert!(limited.solution.residual <= Options::default().tolerance);
}

/// Unlimited must mean unlimited, not zero. A generator with no stated limits
/// is one whose limits are unknown, and clamping it to zero would turn missing
/// data into a voltage collapse that looks like a real finding.
#[test]
fn a_generator_with_no_stated_limits_is_unconstrained() {
    let bus = Bus::generator("g", 0.3, 1.05);

    assert_eq!(bus.q_min, f64::NEG_INFINITY);
    assert_eq!(bus.q_max, f64::INFINITY);

    let mut case = stressed(10.0);
    case.buses[3] = Bus::generator("g", 0.3, 1.05);

    let limited = solve_with_limits(&case, Options::default()).unwrap();
    assert!(limited.binding().is_empty());
    assert!(close(limited.solution.voltages.magnitude[3], 1.05, 1e-9));
}

/// A lower limit binds the same way, with the voltage rising instead of
/// falling — a generator forced to absorb less reactive power than the network
/// wants to push into it.
#[test]
fn a_lower_limit_binds_and_raises_the_voltage() {
    // Light reactive load and a low voltage schedule, so the generator would
    // want to absorb reactive power.
    let case = Case::new(
        "absorbing",
        base(),
        vec![
            Bus::slack("s", 1.05),
            Bus::load("a", -0.1, 0.0),
            Bus::generator("g", 0.1, 0.95).with_reactive_limits(0.0, 1.0),
        ],
        vec![
            Branch::new("s-a", 0, 1, 0.01, 0.05),
            Branch::new("a-g", 1, 2, 0.01, 0.05),
        ],
    );

    let limited = solve_with_limits(&case, Options::default()).unwrap();

    assert_eq!(limited.enforcement[2], Enforcement::AtMinimum);
    assert!(
        close(limited.solution.injections.q[2], 0.0, 1e-6),
        "absorbed {} against a floor of 0",
        limited.solution.injections.q[2]
    );
    assert!(
        limited.solution.voltages.magnitude[2] > 0.95,
        "voltage settled at {} but should have risen above schedule",
        limited.solution.voltages.magnitude[2]
    );
}

#[test]
fn repeated_solves_are_bit_identical() {
    let wanted = solve_ac(&stressed(10.0)).unwrap().injections.q[3];
    let case = stressed(wanted / 2.0);
    let first = solve_with_limits(&case, Options::default()).unwrap();

    for _ in 0..10 {
        assert_eq!(first, solve_with_limits(&case, Options::default()).unwrap());
    }
}

/// A limit cycle is not a convergence failure, and the two call for different
/// responses: a cycle means every solve was fine and the switching heuristic
/// overshot, while a convergence failure means the network may have no
/// solution. Reporting one as the other sends the reader to the wrong place.
#[test]
fn a_limit_cycle_is_reported_as_itself() {
    use bs_powerflow::ac::AcError;

    let error = AcError::LimitCycle { rounds: 10 };
    let message = error.to_string();

    assert!(
        message.contains("did not settle") && message.contains("cycle"),
        "{message}"
    );
    assert!(
        message.contains("equations are fine"),
        "the message must say the solves converged, or it reads as a \
         numerical failure: {message}"
    );

    // And it is a distinct variant, not NotConverged with a sentinel residual.
    assert!(!matches!(error, AcError::NotConverged { .. }));
}
