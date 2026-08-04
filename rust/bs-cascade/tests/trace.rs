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

/// A deficit small enough to dip and recover on damping alone, with no UFLS
/// stage ever crossed.
///
/// Deliberately not [`short_island_case`]: that fixture's 7 pu deficit
/// crosses `two_stage`'s thresholds, and each stage crossing restarts
/// `bs_frequency::ufls::respond`'s integration from rest on a fresh
/// trajectory — so the island's reported `nadir_hz` there is a minimum over
/// several distinct trajectories, while the visible trace is each one
/// truncated at the next stage's crossing. The two are not expected to agree
/// sample-for-sample in that shape of run. A single, never-restarted
/// trajectory is what this test needs to check `decimate` specifically: one
/// continuous integration, so the true minimum of every sample it ever
/// produces is both the whole story and exactly what `nadir_hz` reports.
fn mild_deficit_case() -> Case {
    let base = Base::new(100e6, 345e3).expect("a valid base");

    Case::new(
        "mild",
        base,
        vec![
            Bus::slack("g0", 1.0)
                .with_real_limits(0.0, 3.0)
                .with_machine(2.0, 0.05),
            // 3.05 pu of load against 3.0 pu of capacity: a shortfall small
            // enough that damping alone arrests it well inside the survival
            // band, so nothing here ever calls for a stage.
            Bus::load("l0", -3.05, 0.0),
        ],
        vec![Branch::new("a", 0, 1, 0.01, 0.1).with_rating(20.0)],
    )
}

/// An island with a machine gets a curve, and the curve is the right shape.
///
/// Non-emptiness alone would still pass on a single stray point or on a
/// curve with the wrong values entirely. This checks what a caller actually
/// relies on: the curve starts near nominal (a trace that opened mid-
/// collapse would be a different defect from a missing one), has enough
/// points to be a curve rather than a placeholder, and — the assertion
/// `decimate` exists to make possible — its own lowest point agrees with the
/// `nadir_hz` reported alongside it. That last one is the check that
/// matters: the nadir is displayed as a number beside the curve, and a curve
/// whose minimum disagrees with its own label is exactly the defect
/// decimation (keeping each bucket's min/max rather than sampling blindly)
/// was designed to prevent.
///
/// What would make this fail: `decimate` sampling buckets blindly instead of
/// keeping each bucket's true min/max, which would let the reduced curve's
/// minimum drift away from the run's real nadir.
#[test]
fn a_surviving_island_carries_a_trace() {
    let got = run(&mild_deficit_case(), &Scheme::none(), Limits::default());

    let island = got.reports.first().expect("one island");
    assert!(
        island.trace.len() > 5,
        "expected more than a handful of points, got {}",
        island.trace.len()
    );

    let first = island
        .trace
        .first()
        .expect("a non-empty trace has a first point");
    assert!(
        (first.hz - 60.0).abs() < 0.5,
        "trace should start near nominal (60 Hz), got {} Hz",
        first.hz
    );

    let curve_min = island
        .trace
        .iter()
        .map(|sample| sample.hz)
        .fold(f64::INFINITY, f64::min);
    let nadir = island.nadir_hz.expect("a machine island reports a nadir");

    assert!(
        (curve_min - nadir).abs() < 1e-9,
        "trace minimum {curve_min} Hz disagrees with the reported nadir {nadir} Hz"
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
