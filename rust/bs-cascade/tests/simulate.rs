use bs_cascade::{CascadeOutcome, Event, EventKind, Limits, Options, Verification, simulate};
use bs_case::{Branch, Bus, Case};
use bs_frequency::{IslandCondition, Scheme, Stage};
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
        &Verification::default(),
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
        &Verification::default(),
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
        &Verification::default(),
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
        &Verification::default(),
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
        &Verification::default(),
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
            &Verification::default(),
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
            &Verification::default(),
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
        &Verification::default(),
    );
}

/// One bus, one load ten times bigger than the generator's headroom, and a
/// branch rated so far above the actual flow that it never trips — so
/// nothing in this cascade separates the network, and the frequency model
/// is the only thing that can lose any load.
fn heavy_deficit_case() -> Case {
    let base = Base::new(100e6, 345e3).expect("a valid base");
    let buses = vec![
        Bus::slack("g0", 1.0)
            .with_real_limits(0.0, 3.0)
            .with_machine(4.0, 0.05),
        Bus::load("l0", -10.0, 0.0),
    ];
    let branches = vec![Branch::new("a", 0, 1, 0.01, 0.1).with_rating(20.0)];

    Case::new("heavy", base, buses, branches)
}

/// `load_lost` has two components — load in a de-energized island, and load
/// UFLS shed — and every other test in this file drives `Scheme::none()`, so
/// none of them exercise the second half of that sum at all. A sign error, a
/// swapped index, or a scaling mistake in `shed_fraction * served_before` (see
/// `simulate.rs`) would pass every other test in this file silently, because
/// `shed_total` would just always be `0.0` instead. This test drives a real
/// two-stage scheme through `simulate` and pins the resulting number.
///
/// Two stages are used, not one, because `shed_fraction` compounds: stage 2
/// sheds a fraction of what stage 1 *left*, not a fraction of the original
/// load, and that is exactly where a scaling error (e.g. always shedding a
/// fraction of the original load) would hide without changing whether the
/// test merely passes or fails "some load was lost".
///
/// Both stage thresholds sit a hundredth and two hundredths of a hertz below
/// nominal, with no hold delay, the same technique
/// `bs-cascade/tests/step.rs::a_shed_reduces_load_before_the_power_flow_solves`
/// uses: with an imbalance this large relative to the machine's rating, any
/// nonzero shortfall crosses both thresholds almost immediately, so the test
/// does not depend on hand-solving the swing equation to know whether they
/// fire — only the shed *fractions*, which are exact, need to be hand-checked.
#[test]
fn ufls_shedding_is_counted_in_load_lost() {
    let scheme = Scheme {
        stages: vec![
            Stage {
                at_hz: 59.99,
                shed_fraction: 0.5,
                delay: 0.0,
            },
            Stage {
                at_hz: 59.98,
                shed_fraction: 0.5,
                delay: 0.0,
            },
        ],
        floor_hz: 40.0,
        ceiling_hz: 70.0,
    };

    let got = simulate(
        &heavy_deficit_case(),
        &[],
        &Protection::new(Settings::default(), Vec::new()),
        &scheme,
        &Options::default(),
        &Limits::default(),
        &Verification::default(),
    )
    .expect("solvable");

    let sheds: Vec<&Event> = got
        .events
        .iter()
        .filter(|e| e.kind == EventKind::Shed)
        .collect();
    assert_eq!(sheds.len(), 2, "expected both stages to fire: {sheds:?}");
    assert_eq!(sheds[0].detail, "stage 1 · 50%");
    assert_eq!(sheds[1].detail, "stage 2 · 50%");

    // The branch is rated 20.0 against at most ~10.0 pu of flow, so it never
    // trips, and there is only one branch in the case at all — the network
    // cannot have split. The final case is still the single island it
    // started as, with the slack bus still in it, so the de-energized-island
    // component of load_lost is provably zero here: every bit of load_lost
    // below has to be the UFLS component, not silently dropped in favor of
    // a dead-island figure that happens to be nonzero for some other reason.
    assert_eq!(got.islands.count(), 1);
    assert!(
        got.islands.members[0].contains(&0),
        "the slack bus must still be energized"
    );

    // Hand-computed. Dispatch closes 3.0 pu of the 10.0 pu load (bus0's
    // p_max), leaving a 10.0 - 3.0 = 7.0 pu residual for the frequency
    // model, against a served load of 10.0 pu.
    //
    // Stage 1 sheds 50% of the full 10.0 pu served load: 0.5 x 10.0 = 5.0 pu.
    // Load remaining: 10.0 - 5.0 = 5.0 pu. Residual remaining:
    // 7.0 - 5.0 = 2.0 pu.
    //
    // Stage 2 sheds 50% of what REMAINS — 50% of the now-5.0 pu load, not
    // 50% of the original 10.0: 0.5 x 5.0 = 2.5 pu. Load remaining:
    // 5.0 - 2.5 = 2.5 pu.
    //
    // Total shed: 5.0 + 2.5 = 7.5 pu. Cross-checked against
    // `Response::shed_fraction`, which `simulate` actually multiplies by the
    // pre-shed served load: shed_fraction = (10.0 - 2.5) / 10.0 = 0.75, and
    // 0.75 x 10.0 pu (served_before) = 7.5 pu — the same number.
    assert!(
        (got.load_lost - 7.5).abs() < 1e-6,
        "load_lost {}",
        got.load_lost
    );
}

/// The island's UFLS outcome reaches `Cascade::reports` as its PEAK
/// severity, not merely its condition at the final instant — and not
/// merely `is_some()`, which would pass even if the wrong variant leaked
/// through.
///
/// Reuses `heavy_deficit_case` and the exact two-stage scheme already hand-
/// derived in `ufls_shedding_is_counted_in_load_lost` above, so no new
/// arithmetic is needed here: that test already establishes both stages
/// fire within the cascade's first step, and that dispatch self-corrects
/// on the very next (uncounted) step — the second stage oversheds past the
/// deficit into a surplus, dispatch backs the generator down to match, and
/// the residual imbalance the frequency model sees goes to zero. Left
/// completely alone (`Limits::default()`), this cascade therefore reaches
/// `Settled`, and its terminal response has empty `sheds` and condition
/// `Stable` — a naive "condition = the last instant" implementation would
/// report `Stable` here, discarding the one fact about this island an
/// operator most needs: that it shed 75% of its load along the way.
/// Sticky severity tracking is what keeps that fact in the report even
/// though the island's own last moment was calm.
///
/// `heavy_deficit_case` never splits (one branch, rated far above anything
/// that reaches it), so there is exactly one island throughout and no
/// cross-island attribution to reason about here — see
/// `the_five_bus_golden_reports_each_islands_peak_severity` in
/// `golden_five_bus.rs` for that.
///
/// Fails if `condition` stays `None` (the original gap this fix closed), or
/// comes back `Stable` — which is exactly what reading only the terminal
/// step's response, rather than the peak across the whole run, would
/// produce for this specific cascade.
#[test]
fn a_surviving_islands_condition_reaches_the_report() {
    let scheme = Scheme {
        stages: vec![
            Stage {
                at_hz: 59.99,
                shed_fraction: 0.5,
                delay: 0.0,
            },
            Stage {
                at_hz: 59.98,
                shed_fraction: 0.5,
                delay: 0.0,
            },
        ],
        floor_hz: 40.0,
        ceiling_hz: 70.0,
    };

    let got = simulate(
        &heavy_deficit_case(),
        &[],
        &Protection::new(Settings::default(), Vec::new()),
        &scheme,
        &Options::default(),
        &Limits::default(),
        &Verification::default(),
    )
    .expect("solvable");

    assert_eq!(got.outcome, CascadeOutcome::Settled);
    assert_eq!(got.reports.len(), 1);
    assert_eq!(got.reports[0].condition, Some(IslandCondition::Shedding));
}

/// Two independently-dispatched regions joined by a double-circuit tie:
/// `g0` (slack, `p_max=3.0`) with a 4.0 pu local load `l0`, and `g1` (a
/// second, non-slack generator, `p_max=3.0`) with a 1.0 pu local load `l1`.
/// Losing the tie strands `g0`'s side with more local load than `g0` alone
/// can supply, even though the SYSTEM as a whole had enough capacity —
/// `g1`'s spare 2.0 pu was crossing the tie to help cover `g0`'s own
/// shortfall before the tie was lost.
///
/// `"a"` and `"b"` are each rated generously (6.0, 2.0) so neither ever
/// trips; only the tie is tight.
fn two_generator_case() -> Case {
    let base = Base::new(100e6, 345e3).expect("a valid base");

    let buses = vec![
        Bus::slack("g0", 1.0)
            .with_real_limits(0.0, 3.0)
            .with_machine(4.0, 0.05),
        Bus::load("l0", -4.0, 0.0),
        Bus::generator("g1", 0.0, 1.0)
            .with_real_limits(0.0, 3.0)
            .with_machine(4.0, 0.05),
        Bus::load("l1", -1.0, 0.0),
    ];

    let branches = vec![
        Branch::new("a", 0, 1, 0.01, 0.1).with_rating(6.0),
        Branch::new("tie1", 0, 2, 0.01, 0.1).with_rating(1.0),
        Branch::new("tie2", 0, 2, 0.01, 0.1).with_rating(1.0),
        Branch::new("b", 2, 3, 0.01, 0.1).with_rating(2.0),
    ];

    Case::new("two-generator", base, buses, branches)
}

/// A final island's `condition` survives a REAL mid-run split, attributed
/// to the correct island rather than to whichever island happened to sit at
/// the same array position a step earlier.
///
/// `a_surviving_islands_condition_reaches_the_report` above never
/// re-partitions (`heavy_deficit_case` is one bus pair, one island, always),
/// and the five-bus golden's two final islands land on the SAME condition
/// (`Shedding`, for both — see its own test), so neither can tell correct
/// per-bus attribution apart from a naive positional one
/// (`reports[i].condition = last_responses[i]`): with a constant island
/// count, or with two islands that agree anyway, a positional bug is
/// invisible. This case is built specifically to make that bug visible: the
/// island count changes from 1 to 2 partway through the run, `g0`'s side
/// ends `Shedding` and `g1`'s side ends `Stable`, and only correct
/// bus-indexed attribution can put the right verdict on each.
///
/// # The arithmetic
///
/// **Pre-split (`tie1` open at `t=0`, network still one island through
/// `tie2`).** Dispatch, fresh: combined load `4.0 + 1.0 = 5.0`, combined
/// headroom `3.0 + 3.0 = 6.0` — pro-rata split evenly since both units'
/// headroom is identical: factor `= 5.0 / 6.0`, so both `g0` and `g1` end at
/// `3.0 x 5/6 = 2.5` pu. Gap fully covered (`5.0 <= 6.0`): no residual, no
/// shed. This step's only loggable candidate is the tie: in a radial tree,
/// a branch's flow is exactly the net injection of the subtree beyond it,
/// so the tie carries `g1`'s output minus `l1`'s load: `2.5 - 1.0 = 1.5` pu,
/// flowing from `g1`'s side toward `g0`'s — `g1`'s spare capacity crossing
/// the tie to help `g0`. `tie2` alone (the survivor) now carries the full
/// 1.5 pu against its 1.0 pu rating: `m = 1.5`, the exact loading the
/// five-bus golden's own `tie2` sees, so the same closed form gives the
/// same interval, `t = 0.014 / (1.5^0.02 - 1) ~= 1.7194` s. With no shed to
/// anchor an earlier, faster timeline entry this time, `tie2` is the
/// timeline's only, and therefore earliest, candidate — so it fires in
/// this very first step, splitting the network into `{g0, l0}` (bus 0, 1)
/// and `{g1, l1}` (bus 2, 3).
///
/// **Next step, post-split.** `g0`'s island: gap `= 4.0 - 2.5 = 1.5`,
/// headroom `3.0 - 2.5 = 0.5`, covered up to headroom: `g0 -> 3.0`,
/// residual `= 1.0` — the same 1.0 pu residual the five-bus golden derives,
/// against the same 4.0 pu served load, so the same one-stage, 25% scheme
/// closes it exactly (`0.25 x 4.0 = 1.0`): `g0`'s island sheds and settles
/// `Shedding`. `g1`'s island: gap `= 1.0 - 2.5 = -1.5` (a surplus) —
/// dispatch backs `g1` down to `1.0` exactly, residual `0`, no shed:
/// `Stable`.
///
/// **Final step.** Both islands' gaps are already zero; nothing left to
/// trip or shed. Settled.
///
/// Sticky severity per bus: `g0`'s side sees `Stable` (the pre-split,
/// whole-network step), then `Shedding` — `Shedding` wins. `g1`'s side sees
/// `Stable`, `Stable`, `Stable` throughout — never anything to raise it.
///
/// # What would make this fail
///
/// A regression to positional matching would very plausibly assign island
/// 1's (post-split) condition using whatever sat at array position 1 in an
/// earlier step's `responses` — a step where position 1 didn't exist yet
/// (one combined island) or meant something else entirely — producing
/// `None`, a panic on an out-of-range index, or `g1`'s island wrongly
/// inheriting `Shedding`. A bug that erases severity instead of keeping the
/// maximum (the exact defect fix round 2 corrects) would report `g0`'s
/// island as `Stable`, its last-instant condition, discarding the shed.
#[test]
fn condition_attribution_survives_a_real_split() {
    let scheme = Scheme {
        stages: vec![Stage {
            at_hz: 59.99,
            shed_fraction: 0.25,
            delay: 0.0,
        }],
        floor_hz: 40.0,
        ceiling_hz: 70.0,
    };

    let got = simulate(
        &two_generator_case(),
        &[1], // "tie1"
        &Protection::new(Settings::default(), Vec::new()),
        &scheme,
        &Options::default(),
        &Limits::default(),
        &Verification::default(),
    )
    .expect("solvable");

    assert_eq!(got.outcome, CascadeOutcome::Settled);
    assert_eq!(got.islands.count(), 2);
    assert_eq!(got.reports.len(), 2);

    // g0's island (containing bus 0) sorts first.
    assert_eq!(got.reports[0].members, vec![0, 1]);
    assert_eq!(got.reports[1].members, vec![2, 3]);

    assert_eq!(got.reports[0].condition, Some(IslandCondition::Shedding));
    assert_eq!(got.reports[1].condition, Some(IslandCondition::Stable));
}
