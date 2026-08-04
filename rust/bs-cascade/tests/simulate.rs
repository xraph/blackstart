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

/// The same two-stage cascade as `ufls_shedding_is_counted_in_load_lost`
/// above, but under a REALISTIC survival band (57.0/61.8 Hz) instead of that
/// test's deliberately generous 40.0/70.0 — every other test in this file
/// uses the wide band, which is exactly the shape of gap that let a healthy
/// network's `ceiling_hz` default to `0.0` and read as `Collapsed` upstream,
/// in `bs-solver`, go unnoticed for as long as it did: nothing at the
/// cascade layer ever pushed a settling frequency close enough to a REAL
/// ceiling or floor to notice the band's actual value mattering.
///
/// The two stages, the shed amounts, and the eventual self-correction to a
/// near-nominal settling frequency are all already hand-derived in
/// `ufls_shedding_is_counted_in_load_lost`'s own doc comment — nothing
/// about the physics changes with a narrower band, only whether the FINAL
/// settling frequency happens to land inside it. Since that final settling
/// point is near nominal (60 Hz) once dispatch self-corrects, it is well
/// inside 57.0-61.8 too, so this must reach the same `Settled` outcome and
/// the same 7.5 pu `load_lost` the wide-band version does.
///
/// What would make this fail: any code path that special-cased or hard-coded
/// the wide 40.0/70.0 band seen everywhere else in this file rather than
/// reading `Scheme::floor_hz`/`ceiling_hz` from the scheme actually passed
/// in.
#[test]
fn a_realistic_survival_band_reaches_the_same_outcome() {
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
        floor_hz: 57.0,
        ceiling_hz: 61.8,
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
    assert!(
        (got.load_lost - 7.5).abs() < 1e-6,
        "load_lost {}, want 7.5 pu — unchanged by narrowing the band",
        got.load_lost
    );
}

/// Every relay disabled: an infinite overcurrent pickup and no distance
/// zones at all.
///
/// The two `load_lost` tests below need the island to stay whole — generator
/// and load together — for the ENTIRE run, so that a frequency collapse is
/// the only way anything gets lost. `heavy_deficit_case`'s sustained heavy
/// loading, left unshed for long enough, is itself enough to pick up a zone-2
/// distance trip around t = 0.35s (found while writing these tests, by
/// printing `Cascade::events` for exactly this fixture and scheme) — which
/// would split the load onto its own de-energized island and let the
/// PRE-FIX `dead_load` (counting only `Subcase::extract(..).is_none()`
/// islands) get the right answer for the wrong reason, making the test
/// unable to fail against the bug it exists to catch. Disabling protection
/// here keeps the one thing under test — the frequency-collapse path —
/// isolated from a second, unrelated way this fixture can lose its load.
fn disabled_protection() -> Protection {
    Protection::new(
        Settings {
            overcurrent: bs_protection::Overcurrent {
                pickup: f64::INFINITY,
                instantaneous: None,
                ..bs_protection::Overcurrent::default()
            },
            distance: bs_protection::Distance {
                zones: Vec::new(),
                breaker_time: 0.05,
            },
        },
        Vec::new(),
    )
}

/// An island that ends `Collapsed` loses ALL of its load, even though it
/// never de-energizes — its slack bus keeps its machine the whole run.
///
/// Reuses `heavy_deficit_case` with NO shedding scheme at all
/// (`Scheme::none()`), so `Model::settling`'s closed form gives the exact
/// number directly, with no stage arithmetic in the way, and with
/// `disabled_protection` (see its own doc) so nothing trips the branch and
/// splits the load off before the frequency model ever gets a say. Dispatch
/// closes 3.0 pu of the 10.0 pu deficit (bus0's `p_max`), leaving a 7.0 pu
/// residual for the frequency model — the same figure
/// `ufls_shedding_is_counted_in_load_lost` above hand-derives. With no
/// governor response beyond `g0`'s own explicit R = 0.05
/// (`inverse_droop` = 3.0/0.05 / 3.0 = 20.0 pu) and load damping D = 1.0,
/// `Δf = -imbalance / (D + 1/R) = -7.0 / 21.0 = -0.3333...` pu, ×60 Hz
/// nominal = -20.0 Hz: the island settles at 40.0 Hz, below `Scheme::none()`'s
/// 57.0 Hz floor — `Collapsed`, on the very first step, with the slack bus
/// still attached and still producing 3.0 pu.
///
/// `load_lost` must therefore be the island's WHOLE served load, 10.0 pu —
/// not the 0.0 the pre-fix code reported, because `Subcase::extract` still
/// succeeds for an island that kept its slack bus, and the old `dead_load`
/// loop only ever counted islands where extraction failed.
///
/// What would make this fail: `dead_load` (in `bs-cascade/src/simulate.rs`)
/// reverting to counting only `Subcase::extract(..).is_none()` islands,
/// which would bring `load_lost` back to 0.0 on this fixture even though the
/// outcome is `Collapsed`.
#[test]
fn a_collapsed_island_that_keeps_its_generator_loses_its_full_load() {
    let got = simulate(
        &heavy_deficit_case(),
        &[],
        &disabled_protection(),
        &Scheme::none(),
        &Options::default(),
        &Limits::default(),
        &Verification::default(),
    )
    .expect("solvable");

    assert_eq!(got.outcome, CascadeOutcome::Collapsed);
    assert_eq!(got.islands.count(), 1);
    assert!(
        got.islands.members[0].contains(&0),
        "the slack bus must still be energized — this is a frequency \
         collapse, not a de-energized island"
    );
    assert!(
        (got.load_lost - 10.0).abs() < 1e-6,
        "load_lost {}, want 10.0 pu — the island's entire served load",
        got.load_lost
    );
}

/// The compounding half of the same bug: the step that BOTH sheds AND
/// collapses must still land its shed in `load_lost`, even though `simulate`
/// breaks out of its loop on that exact step, before the "normal" path that
/// records a step's effects would otherwise run.
///
/// One stage, threshold 59.99 Hz with no hold delay — the same threshold
/// `ufls_shedding_is_counted_in_load_lost` above already establishes fires
/// within this fixture's first step, given the same 7.0 pu residual. It
/// sheds 10% of the 10.0 pu served load: 1.0 pu, leaving a 9.0 pu load and
/// reducing the residual imbalance to 7.0 - 1.0 = 6.0 pu. Re-settling against
/// that: `Δf = -6.0 / 21.0 x 60 = -17.1428...` Hz, settling at
/// 60 - 17.1428... ≈ 42.857 Hz — below this test's own 50.0 Hz floor, so the
/// SAME step that sheds is also the step that collapses.
///
/// `load_lost` must be the full original 10.0 pu: 9.0 pu remaining
/// (post-shed, still sitting on the case, read back by `dead_load`) plus the
/// 1.0 pu this step shed. Losing the shed half — the exact bug the loop
/// re-ordering in `simulate` fixes — would report 9.0 pu instead.
///
/// What would make this fail: reverting the `simulate` loop so `record_step`
/// runs only in the non-collapsing path, once again dropping the collapse-
/// triggering step's own shed from `shed_total` — `load_lost` would come
/// back 9.0 instead of 10.0.
#[test]
fn a_shed_on_the_collapsing_step_itself_is_not_dropped() {
    let scheme = Scheme {
        stages: vec![Stage {
            at_hz: 59.99,
            shed_fraction: 0.1,
            delay: 0.0,
        }],
        floor_hz: 50.0,
        ceiling_hz: 61.8,
    };

    let got = simulate(
        &heavy_deficit_case(),
        &[],
        &disabled_protection(),
        &scheme,
        &Options::default(),
        &Limits::default(),
        &Verification::default(),
    )
    .expect("solvable");

    assert_eq!(got.outcome, CascadeOutcome::Collapsed);

    let sheds: Vec<&Event> = got
        .events
        .iter()
        .filter(|e| e.kind == EventKind::Shed)
        .collect();
    assert_eq!(sheds.len(), 1, "the one stage must have fired: {sheds:?}");

    assert!(
        (got.load_lost - 10.0).abs() < 1e-6,
        "load_lost {}, want 10.0 pu (9.0 remaining + 1.0 shed on the \
         collapsing step)",
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

/// The numeric frequency figures — nadir, rate of change, settling
/// frequency, and shed — reach the report the same way `condition` already
/// does, not merely `condition` on its own.
///
/// Reuses `heavy_deficit_case` and the same shed fractions
/// `ufls_shedding_is_counted_in_load_lost` hand-derives — 50% then 50% of
/// what is left, 7.5 pu total — but NOT that test's zero hold delay.
/// `nadir_hz` is derived from the assembled `trace` (the truncated path the
/// island actually lived, per `bs_frequency::ufls::respond`), not from
/// either trajectory's own unbounded integration horizon. With a zero delay
/// each stage's crossing fires on the very first post-`t=0` sample, and
/// `push_segment` truncates a segment strictly BEFORE its own crossing
/// sample — so the real, lived portion of segments 1 and 2 is only their
/// shared `t = 0` point, still exactly at nominal. This fixture's leftover
/// imbalance after both sheds turns out to be a mild SURPLUS, not a deficit
/// (halving a 10 pu load twice against a slack capped at 3 pu overshoots
/// past balance), so the one segment that does run a real span only ever
/// rises. That is a real trace, but it never actually visits a frequency
/// below nominal at all — the old, pre-fix number that looked like a nadir
/// here was the counterfactual tail of the very first (unshed) trajectory,
/// exactly the bug this test now exists to not paper over.
///
/// A 0.05 s hold delay (measured against this fixture's own output, not
/// hand-solved from the swing equation — the same shortcut the zero-delay
/// version already relied on to avoid that derivation) is enough: this
/// island's initial `RoCoF` is steep enough that the deviation is already
/// below both thresholds within the first 0.01 s step and keeps falling for
/// the whole 0.05 s hold, so segment 1 alone carries a real, sub-nominal
/// excursion down to ~57.43 Hz before the first shed actually fires, at
/// t ≈ 0.06 s.
///
/// Fails if: `shed` stays `0.0` (the field never wired up at all — the
/// original defect this test exists to close); `shed` is nonzero but not
/// 7.5 (attribution or double-counting bug); `nadir_hz` is `None` or not
/// comfortably below nominal — a real deficit that never dipped below
/// nominal is not what this fixture produces; `rocof_hz_s` is `None` or
/// `0.0` — the initial rate of change for a real step imbalance is never
/// exactly zero; or `settling_hz` is `None`.
#[test]
fn reports_frequency_numbers_are_populated_when_ufls_fires() {
    let scheme = Scheme {
        stages: vec![
            Stage {
                at_hz: 59.99,
                shed_fraction: 0.5,
                delay: 0.05,
            },
            Stage {
                at_hz: 59.98,
                shed_fraction: 0.5,
                delay: 0.05,
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

    assert_eq!(got.reports.len(), 1);
    let report = &got.reports[0];

    assert!(
        (report.shed - 7.5).abs() < 1e-6,
        "shed {} pu, expected 7.5 pu — the whole cascade's load_lost, since \
         this island never splits and never loses its slack",
        report.shed
    );

    // ~57.43 Hz measured with this fixture's 0.05 s hold — see the doc
    // comment above. 58.0 leaves margin without being so loose that a
    // regression back to "never dips at all" (60.0, the pre-fix bug for this
    // exact fixture) would slip through.
    let nadir = report.nadir_hz.expect("a real deficit reaches a nadir");
    assert!(
        nadir < 58.0,
        "nadir {nadir} Hz should be a real sub-nominal excursion, not merely \
         a hair below nominal"
    );

    let rocof = report
        .rocof_hz_s
        .expect("a real deficit has an initial rate of change");
    assert!(
        rocof.abs() > 1e-9,
        "rocof came back approximately zero: {rocof}"
    );

    assert!(report.settling_hz.is_some());
}

/// `g0` (slack, machine) feeds `l0` over branch `"a"`, which stays in
/// service throughout; `l1` hangs off `l0` over branch `"tie"` alone, which
/// is the initiating outage in every test that uses this fixture.
///
/// `"tie"` opens as the initiating outage, before `step` is ever called for
/// the first time — so unlike `three_line_case`'s parallel branches, which
/// stay connected for at least one step before tripping, `l1` is severed
/// from every machine in the case from the very first `step` call onward,
/// never transiently sharing an island with `g0`. That distinction is the
/// entire point of this fixture: a bus that was *ever* briefly part of a
/// machine's island, even for one step before a later split, sticks with
/// whatever condition that step observed — the same "peak severity, not
/// last instant" rule `IslandReport::condition` documents — so asserting
/// `None` against a bus that only became machine-less partway through a run
/// would not test what "never observed with a machine" means; it would test
/// "the running severity happened to still be `None`" purely by chance.
/// `l1` here never has that chance.
fn stranded_load_case() -> Case {
    let base = Base::new(100e6, 345e3).expect("a valid base");
    let buses = vec![
        Bus::slack("g0", 1.0)
            .with_real_limits(0.0, 3.0)
            .with_machine(4.0, 0.05),
        Bus::load("l0", -0.5, 0.0),
        Bus::load("l1", -0.3, 0.0),
    ];
    let branches = vec![
        Branch::new("a", 0, 1, 0.01, 0.1).with_rating(2.0),
        Branch::new("tie", 1, 2, 0.01, 0.1).with_rating(2.0),
    ];

    Case::new("stranded", base, buses, branches)
}

/// A machine-less island's frequency fields stay ABSENT, not zero — the same
/// distinction `condition` already draws, and for the same reason: an
/// island nobody supplied inertia data for has no frequency to report, which
/// is not the same claim as "its frequency is zero". `shed` alone among the
/// four is a plain `0.0` on this island, which is correct — it really did
/// shed nothing, `Option` or not, since a machine-less island cannot run
/// UFLS at all.
///
/// Fails if a machine-less island's `nadir_hz`/`rocof_hz_s`/`settling_hz`
/// ever comes back `Some(0.0)` instead of `None` — the exact shape of bug
/// `condition` already had to be guarded against before this task, now
/// repeated across three more fields that could each independently regress
/// the same way.
///
/// `dead.shed`'s `0.0` is compared exactly rather than with an epsilon: it
/// is `BusHistory::default()`'s own untouched value on a bus this run never
/// once attributes a shed to, not the result of any arithmetic — the same
/// verbatim-passthrough reasoning `bs-case`'s own constructor tests document
/// for `#![allow(clippy::float_cmp)]`.
#[test]
#[allow(clippy::float_cmp)]
fn a_machineless_islands_frequency_fields_stay_absent() {
    let got = simulate(
        &stranded_load_case(),
        &[1], // "tie"
        &Protection::new(Settings::default(), Vec::new()),
        &Scheme::none(),
        &Options::default(),
        &Limits::default(),
        &Verification::default(),
    )
    .expect("solvable");

    assert_eq!(got.outcome, CascadeOutcome::Settled);
    assert_eq!(got.reports.len(), 2);

    let dead = got
        .reports
        .iter()
        .find(|r| r.members == vec![2])
        .expect("bus 2, l1, ends up alone in its own island");

    assert_eq!(dead.condition, None);
    assert_eq!(dead.nadir_hz, None);
    assert_eq!(dead.rocof_hz_s, None);
    assert_eq!(dead.settling_hz, None);
    assert_eq!(dead.shed, 0.0);

    // And the surviving island, which DOES keep a machine throughout,
    // reports the opposite: a real condition, not absence.
    let alive = got
        .reports
        .iter()
        .find(|r| r.members == vec![0, 1])
        .expect("g0 and l0 stay together the whole run");
    assert!(alive.condition.is_some());
}

/// `g0` (slack, `H = 4.0`, `R = 0.05`, `p_max = 3.0`) feeds `l0` (`-4.0` pu)
/// over the ONLY path, branch `"a"` (`r = 0.01`, `x = 0.1`, rated `1.0`).
///
/// Sized so the branch's overcurrent element trips on its INSTANTANEOUS
/// element, not the inverse-time curve, closing the numbers exactly with no
/// relay-curve algebra needed: `"a"` carries the network's whole `4.0` pu
/// load (the only path from `g0` to `l0`), a loading of `4.0` against a
/// `1.0` pu rating — past the default instantaneous multiple of `2.5` — so
/// it trips at exactly the default breaker time, `0.05` s, deterministically.
/// (Distance protection stays out of it: at `x = 0.1`, even zone 3's reach
/// of `1.5` requires a flow past `1 / (1.5 x 0.1) ~= 6.67` pu to see it, well
/// above the `4.0` pu actually flowing.)
fn quiet_split_case() -> Case {
    let base = Base::new(100e6, 345e3).expect("a valid base");
    let buses = vec![
        Bus::slack("g0", 1.0)
            .with_real_limits(0.0, 3.0)
            .with_machine(4.0, 0.05),
        Bus::load("l0", -4.0, 0.0),
    ];
    let branches = vec![Branch::new("a", 0, 1, 0.01, 0.1).with_rating(1.0)];

    Case::new("quiet-split", base, buses, branches)
}

/// THE TEST THIS TASK EXISTS FOR, one level up from
/// `bs-frequency`'s own `nadir_hz_is_the_minimum_of_the_trace`.
///
/// That fix made `bs_frequency::ufls::respond` derive `Response::nadir_hz`
/// from its OWN assembled `trace` rather than a raw trajectory's whole
/// (possibly counterfactual) integration horizon. But `bs_cascade`'s
/// `accumulate_bus_history` has a second, independent truncation on top of
/// that one: `bound`, which cuts `history.trace` off at `out.at` — the
/// EARLIEST event anywhere in the whole cascade this step, not anything
/// specific to this particular island. Before this fix, `history.nadir_hz`
/// was folded in from `nominal + response.nadir_hz` — the WHOLE step's own
/// minimum, ignoring `bound` entirely — so it could name a frequency past
/// the instant `bound` says this island's contribution to `trace` stops.
///
/// This fixture exercises exactly that gap. `quiet_split_case` is ONE island
/// (both buses) for its first and only step, and `l0` never sheds and never
/// trips anything itself — it is the passive party. What ends the step is
/// `"a"`'s OWN overcurrent element, at `t = 0.05` s (see the fixture's own
/// doc for why that number is exact rather than measured). `out.at = 0.05`
/// for the whole step, so `bound = Some(0.05)` truncates BOTH buses'
/// contribution to `history.trace` there — well before `g0`+`l0`'s combined
/// swing (residual `= 4.0 - 3.0 = 1.0` pu after dispatch, the same
/// `heavy_deficit_case`-style arithmetic used throughout this file) reaches
/// its own true nadir. That true nadir is a real, physically legitimate
/// point on the UNTRUNCATED trajectory — this is not a shed-crossing
/// counterfactual, the thing `bs-frequency`'s own fix addressed — it is
/// simply LATER than the instant this island's real, lived history stops:
/// at `t = 0.05` s the case is re-partitioned into `{g0}` alone (no load,
/// flat at nominal from then on) and `{l0}` alone, which has no PV or slack
/// member at all, so `Aggregate::of` returns `None` and `l0` never gets
/// another frequency response for the rest of the run. Its bus's history is
/// therefore fixed forever at whatever this ONE step contributed — the
/// clean test for whether that contribution was bound-truncated correctly,
/// with nothing later to paper over getting it wrong.
///
/// Confirmed against this fixture's own instrumented output (measured, not
/// hand-solved — the swing here has governor reheat dynamics, so the exact
/// transient nadir has no closed form, the same reasoning already used
/// elsewhere in this file for a transient nadir/relay time):
/// - The raw, UNBOUNDED `response.nadir_hz` for this one step's combined
///   island is a deviation of `-6.256373831582707` Hz (`53.74362616841729`
///   Hz absolute) — the counterfactual "if the split had never happened"
///   figure the pre-fix code reported for BOTH resulting buses.
/// - The bound-truncated slice — `t < 0.05` s, 5 samples at the `0.01` s
///   integration step — bottoms out at `59.70520645730025` Hz absolute, the
///   real, lived value. `l0`'s report's `trace` has exactly 5 points (under
///   `TRACE_BUCKETS`, so `decimate` returns it unchanged — see
///   `decimate.rs::a_trace_shorter_than_the_budget_is_unchanged` — so
///   asserting against the WIRE-shaped `trace` here is already exact, not
///   merely "sound after decimation" as it would be for a longer curve).
#[test]
fn nadir_hz_is_the_minimum_of_the_trace_across_a_split() {
    let got = simulate(
        &quiet_split_case(),
        &[],
        &Protection::new(Settings::default(), Vec::new()),
        &Scheme::none(),
        &Options::default(),
        &Limits::default(),
        &Verification::default(),
    )
    .expect("solvable");

    // Topologically, not thermally, collapsed: `l0` ends up on a
    // load-bearing island with no machine at all. `is_collapsed`'s own doc
    // treats that the same as a frequency collapse — expected here, and not
    // what this test is about.
    assert_eq!(got.outcome, CascadeOutcome::Collapsed);
    assert_eq!(got.reports.len(), 2);

    let dead = got
        .reports
        .iter()
        .find(|r| r.members == vec![1])
        .expect("l0 ends up alone, machine-less, after the split");

    assert!(
        !dead.trace.is_empty(),
        "the dead island must still carry the one real segment it lived \
         through before losing its machine"
    );

    let trace_min_hz = dead
        .trace
        .iter()
        .map(|sample| sample.hz)
        .fold(f64::INFINITY, f64::min);
    let nadir = dead
        .nadir_hz
        .expect("l0 had a real machine-bearing island for one whole step");

    // Bit-for-bit: both are folds over the very same samples, so a correct
    // implementation reproduces the identical `f64` — the same standard
    // `bs-frequency`'s own `nadir_hz_is_the_minimum_of_the_trace` already
    // holds `respond` to, one level up.
    assert_eq!(
        nadir.to_bits(),
        trace_min_hz.to_bits(),
        "nadir_hz ({nadir}) must equal the reported trace's own minimum \
         ({trace_min_hz} Hz) — a curve that disagreed with the number \
         printed beside it would be worse than a coarse one"
    );

    // And pin the actual number, not merely the equality: an implementation
    // that made both sides consistently wrong (e.g. by deriving BOTH from
    // the unbounded trajectory) would still pass the check above. `59.0`
    // sits strictly between the real, bound-truncated value (~59.71 Hz) and
    // the counterfactual, unbounded one (~53.74 Hz) this fixture's own
    // measured output puts on either side of it.
    assert!(
        nadir > 59.0,
        "nadir {nadir} Hz should be the mild, real dip this fixture's \
         0.05 s truncation actually allows (~59.71 Hz measured), not the \
         deep, counterfactual figure the whole 60 s horizon would show if \
         the split had never happened (~53.74 Hz measured)"
    );
}
