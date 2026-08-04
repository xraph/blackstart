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

/// A network that loses everything is a collapse.
///
/// Rating 0.1 means both survivors of the initiating outage are overloaded
/// by 5x and trip together in the first real step, taking out every branch
/// and leaving a load bus with no generation anywhere in its island.
#[test]
fn losing_every_island_is_a_collapse() {
    let got = simulate(
        &three_line_case(0.1),
        &[0],
        &Protection::new(Settings::default(), Vec::new()),
        &Scheme::none(),
        &Options::default(),
        &Limits::default(),
    )
    .expect("solvable");

    assert_eq!(got.outcome, CascadeOutcome::Collapsed);
}

/// Served load never rises across a cascade: allowing more steps can only
/// lose as much or more load than allowing fewer, never less.
///
/// The brief's version of this test tried to read a shed amount back out of
/// `detail`, a human-readable label like `"stage 1 · 10%"` — `split_whitespace()
/// .next()` yields `"stage"`, which never parses as a number, so `unwrap_or`
/// always supplied 0.0 and the running total never moved. The assertion
/// could not fail.
///
/// This replaces it with a property that is structural rather than textual.
/// `load_lost` is computed once, from the final state, from a case where
/// trips only ever take a branch out of service and shedding only ever
/// scales a load down — never the reverse. So re-running the identical
/// cascade with a larger step budget can only ever *reveal* more of that
/// same one-way process, never less of it. A version of `load_lost` that
/// double-counted a newly-dead island against an already-counted shed record
/// (exactly the two-places-at-once bug the brief warns `load_lost` must
/// avoid) would very plausibly make some larger prefix report a smaller
/// total than a shorter one already did — which is exactly what this test
/// checks cannot happen.
#[test]
fn served_load_is_monotonically_non_increasing() {
    let case = three_line_case(0.4);
    let initiating = [0];
    let protection = Protection::new(Settings::default(), Vec::new());
    let scheme = Scheme::none();
    let options = Options::default();

    let mut previous_loss = 0.0;
    for max_steps in 0..=6 {
        let got = simulate(
            &case,
            &initiating,
            &protection,
            &scheme,
            &options,
            &Limits {
                max_steps,
                horizon: 3600.0,
            },
        )
        .expect("solvable");

        assert!(
            got.load_lost + 1e-9 >= previous_loss,
            "allowing {max_steps} steps lost {} pu, less than the {} pu a tighter \
             budget already lost — load was restored",
            got.load_lost,
            previous_loss
        );
        previous_loss = got.load_lost;
    }

    // The case's only load is 1.0 pu at l0 (Bus::load("l0", -1.0, 0.0)), so
    // load_lost can never exceed 1.0 — there is nothing more to lose.
    assert!(previous_loss <= 1.0 + 1e-9, "load_lost {previous_loss}");
}

/// The same input produces a byte-identical event sequence, every time.
///
/// This is the determinism claim made concrete. It is the test that catches a
/// `HashMap` that crept onto a numeric path, or a sort whose tie-break
/// depends on collection order.
#[test]
fn the_event_sequence_is_reproducible() {
    let case = three_line_case(0.4);
    let run = || {
        simulate(
            &case,
            &[0],
            &Protection::new(Settings::default(), Vec::new()),
            &Scheme::none(),
            &Options::default(),
            &Limits::default(),
        )
        .expect("solvable")
    };

    let first = run();
    let second = run();

    assert_eq!(first.events, second.events);
    assert_eq!(first.load_lost.to_bits(), second.load_lost.to_bits());
    assert_eq!(first.outcome, second.outcome);
}

/// An initiating outage naming a branch that does not exist is a caller
/// mistake worth naming, not a silent no-op.
#[test]
#[should_panic(expected = "initiating outage names branch")]
fn an_out_of_range_initiating_outage_panics() {
    let _ = simulate(
        &three_line_case(1.0),
        &[99],
        &Protection::new(Settings::default(), Vec::new()),
        &Scheme::none(),
        &Options::default(),
        &Limits::default(),
    );
}
