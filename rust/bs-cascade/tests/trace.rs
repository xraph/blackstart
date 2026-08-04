use bs_cascade::{Limits, Options, Verification, simulate};
use bs_case::{Branch, Bus, Case};
use bs_frequency::{Scheme, Stage};
use bs_protection::{Protection, Settings};
use bs_quantity::Base;

/// A generator well short of its load, so UFLS fires.
fn short_island_case() -> Case {
    let base = Base::new(100e6, 345e3).expect("a valid base");

    Case::new(
        "short",
        base,
        vec![
            Bus::slack("g0", 1.0)
                .with_real_limits(0.0, 3.0)
                .with_machine(2.0, 0.05),
            Bus::load("l0", -10.0, 0.0),
        ],
        vec![Branch::new("a", 0, 1, 0.01, 0.1).with_rating(20.0)],
    )
}

fn two_stage() -> Scheme {
    Scheme {
        stages: vec![
            Stage {
                at_hz: 59.3,
                shed_fraction: 0.5,
                delay: 0.1,
            },
            Stage {
                at_hz: 58.9,
                shed_fraction: 0.5,
                delay: 0.1,
            },
        ],
        floor_hz: 57.0,
        ceiling_hz: 61.8,
    }
}

fn run(case: &Case, scheme: &Scheme, limits: Limits) -> bs_cascade::Cascade {
    simulate(
        case,
        &[],
        &Protection::new(Settings::default(), Vec::new()),
        scheme,
        &Options::default(),
        &limits,
        &Verification::default(),
    )
    .expect("solvable")
}

/// An island with a machine gets a curve.
#[test]
fn a_surviving_island_carries_a_trace() {
    let got = run(&short_island_case(), &two_stage(), Limits::default());

    let island = got.reports.first().expect("one island");
    assert!(
        !island.trace.is_empty(),
        "an island with a machine has a curve"
    );
}

/// The trace is on the RUN clock, not each step's local clock.
///
/// Every `respond` call starts its own trace at zero. If the step offset were
/// dropped, a multi-step cascade would emit several curves all starting at
/// zero and the assembled trace would jump backwards at every step boundary —
/// the same defect as the intra-response overlap, one level up.
#[test]
fn the_trace_is_on_the_run_clock_and_never_goes_backwards() {
    let got = run(&short_island_case(), &two_stage(), Limits::default());

    for report in &got.reports {
        for pair in report.trace.windows(2) {
            assert!(
                pair[1].at >= pair[0].at,
                "trace went backwards: {}s follows {}s",
                pair[1].at,
                pair[0].at
            );
        }
    }
}

/// An island with no machine has NO curve — not a flat line at zero.
///
/// A zero-hertz curve renders as a collapsed island. Absent must stay absent,
/// which is the rule every other frequency field on this path already follows.
#[test]
fn a_machineless_island_has_no_trace() {
    let base = Base::new(100e6, 345e3).expect("a valid base");
    let mut case = Case::new(
        "stranded",
        base,
        vec![
            Bus::slack("g0", 1.0)
                .with_real_limits(0.0, 3.0)
                .with_machine(4.0, 0.05),
            Bus::load("l0", -0.5, 0.0),
            Bus::load("l1", -0.5, 0.0),
        ],
        vec![
            Branch::new("a", 0, 1, 0.01, 0.1).with_rating(5.0),
            Branch::new("tie", 1, 2, 0.01, 0.1).with_rating(5.0),
        ],
    );
    case.branches[1].in_service = false;

    let got = run(&case, &Scheme::none(), Limits::default());

    let stranded = got
        .reports
        .iter()
        .find(|r| !r.members.contains(&0))
        .expect("an island without the generator");

    assert!(
        stranded.trace.is_empty(),
        "no machine means no curve, not a flat zero"
    );
}

/// Same input, same curve.
#[test]
fn the_accumulated_trace_is_reproducible() {
    let case = short_island_case();
    let first = run(&case, &two_stage(), Limits::default());
    let second = run(&case, &two_stage(), Limits::default());

    for (a, b) in first.reports.iter().zip(second.reports.iter()) {
        assert_eq!(a.trace.len(), b.trace.len());
        for (p, q) in a.trace.iter().zip(b.trace.iter()) {
            assert_eq!(p.at.to_bits(), q.at.to_bits());
            assert_eq!(p.hz.to_bits(), q.hz.to_bits());
        }
    }
}
