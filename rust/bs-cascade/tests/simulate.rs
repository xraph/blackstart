use bs_cascade::{CascadeOutcome, EventKind, Limits, Options, simulate};
use bs_case::{Branch, Bus, Case};
use bs_frequency::Scheme;
use bs_protection::{Protection, Settings};
use bs_quantity::Base;

/// `Base` lives in `bs_quantity`, not `bs_case` — matching
/// `rust/bs-cascade/tests/step.rs`, which already gets this right.
fn three_line_case(rating: f64) -> Case {
    let base = Base::new(100e6, 345e3).expect("a valid base");
    let buses = vec![
        Bus::slack("g0", 1.0)
            .with_real_limits(0.0, 3.0)
            .with_machine(4.0, 0.05),
        Bus::load("l0", -1.0, 0.0),
    ];
    let branches = (0..3)
        .map(|n| Branch::new(format!("ln{n}"), 0, 1, 0.01, 0.1).with_rating(rating))
        .collect();

    Case::new("three", base, buses, branches)
}

/// No initiating outage, healthy network: settled at step zero with nothing
/// in the log.
#[test]
fn a_healthy_network_settles_immediately() {
    let got = simulate(
        &three_line_case(1.0),
        &[],
        &Protection::new(Settings::default(), Vec::new()),
        &Scheme::none(),
        &Options::default(),
        &Limits::default(),
    )
    .expect("solvable");

    assert_eq!(got.outcome, CascadeOutcome::Settled);
    assert_eq!(got.steps, 0);
    assert!((got.load_lost - 0.0).abs() < 1e-12);
}

/// Three lines each rated 0.4 carry 0.333 apiece. Lose one and the survivors
/// sit at 0.5 — over rating — so they trip, and the load is lost.
///
/// The brief's version of this test asserted only that *a* `Trip` event
/// exists, but `simulate` always logs the initiating outage itself as a
/// `Trip` event at t = 0 — so that assertion held for any `initiating`
/// slice at all, propagation or not, and could never fail on a loop that
/// did nothing further. This counts trips instead: strictly more than the
/// one initiating trip is only possible if the cascade actually propagated.
#[test]
fn an_initiating_outage_propagates() {
    let got = simulate(
        &three_line_case(0.4),
        &[0],
        &Protection::new(Settings::default(), Vec::new()),
        &Scheme::none(),
        &Options::default(),
        &Limits::default(),
    )
    .expect("solvable");

    assert!(got.steps > 0, "the cascade must take at least one step");

    let trips = got
        .events
        .iter()
        .filter(|e| e.kind == EventKind::Trip)
        .count();
    assert!(
        trips > 1,
        "only the initiating trip was logged ({trips}); the cascade did not propagate"
    );

    assert!(got.load_lost > 0.0, "separated load must be counted");
}

/// Events are non-decreasing in simulated time.
///
/// The console's schema enforces exactly this and rejects a run that breaks
/// it, so a violation here is a run the operator console will refuse to
/// render. It is also the property the one-clock design exists to guarantee.
#[test]
fn events_are_ordered_in_simulated_time() {
    let got = simulate(
        &three_line_case(0.4),
        &[0],
        &Protection::new(Settings::default(), Vec::new()),
        &Scheme::none(),
        &Options::default(),
        &Limits::default(),
    )
    .expect("solvable");

    for pair in got.events.windows(2) {
        assert!(
            pair[1].at >= pair[0].at,
            "{}s follows {}s",
            pair[1].at,
            pair[0].at
        );
    }
}

/// A run that hits the step cap is truncated, never settled.
///
/// Reporting an interrupted cascade as a settled one inverts its meaning, the
/// same way `Verdict::Insolvable` outranks `Secure`.
#[test]
fn hitting_the_step_cap_is_truncated_not_settled() {
    let got = simulate(
        &three_line_case(0.4),
        &[0],
        &Protection::new(Settings::default(), Vec::new()),
        &Scheme::none(),
        &Options::default(),
        &Limits {
            max_steps: 1,
            horizon: 3600.0,
        },
    )
    .expect("solvable");

    assert_eq!(got.outcome, CascadeOutcome::Truncated { steps: 1 });
}
