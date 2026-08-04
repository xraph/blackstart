use bs_cascade::{Options, step};
use bs_case::{Branch, Bus, Case};
use bs_frequency::Scheme;
use bs_protection::{Protection, Settings};
use bs_quantity::Base;

/// Two parallel lines from a generator to a load. Each rated to carry the
/// whole load alone, so nothing trips.
fn parallel_case(rating: f64) -> Case {
    let base = Base::new(100e6, 345e3).expect("a valid base");
    let buses = vec![
        Bus::slack("g0", 1.0)
            .with_real_limits(0.0, 3.0)
            .with_machine(4.0, 0.05),
        Bus::load("l0", -1.0, 0.0),
    ];
    let branches = vec![
        Branch::new("a", 0, 1, 0.01, 0.1).with_rating(rating),
        Branch::new("b", 0, 1, 0.01, 0.1).with_rating(rating),
    ];

    Case::new("parallel", base, buses, branches)
}

/// An unstressed network produces no trips.
#[test]
fn a_healthy_network_fires_nothing() {
    let mut case = parallel_case(1.0);
    let got = step(
        &mut case,
        &Protection::new(Settings::default(), Vec::new()),
        &Scheme::none(),
        &Options::default(),
    )
    .expect("a solvable case");

    assert!(got.fired.is_empty());
    assert_eq!(got.islands.count(), 1);
}

/// Losing one of two parallel lines doubles the other's flow. With a rating
/// of 0.6 the survivor sits at 1.67 pu and its protection operates.
#[test]
fn the_survivor_of_a_parallel_pair_trips() {
    let mut case = parallel_case(0.6);
    case.branches[0].in_service = false;

    let got = step(
        &mut case,
        &Protection::new(Settings::default(), Vec::new()),
        &Scheme::none(),
        &Options::default(),
    )
    .expect("a solvable case");

    assert_eq!(got.fired.len(), 1);
    assert_eq!(got.fired[0].branch, 1);
    assert!(got.at > 0.0);
}

/// Protection disabled means no trips, whatever the loading.
///
/// This is the test that catches a threshold applied somewhere other than
/// where candidates are collected: if any downstream code has its own idea of
/// "overloaded", this fails.
#[test]
fn disabled_protection_fires_nothing_however_loaded() {
    let mut case = parallel_case(0.01);
    case.branches[0].in_service = false;

    let disabled = Settings {
        overcurrent: bs_protection::Overcurrent {
            pickup: f64::INFINITY,
            instantaneous: None,
            ..bs_protection::Overcurrent::default()
        },
        distance: bs_protection::Distance {
            zones: Vec::new(),
            breaker_time: 0.05,
        },
    };

    let got = step(
        &mut case,
        &Protection::new(disabled, Vec::new()),
        &Scheme::none(),
        &Options::default(),
    )
    .expect("a solvable case");

    assert!(got.fired.is_empty(), "fired {:?}", got.fired);
}

/// Two radial branches off the same slack, each carrying its own load to its
/// own bus — no splitting between them, so each branch's loading is exactly
/// controlled by that bus's load and that branch's rating, with no power-flow
/// arithmetic needed to know it. Branch `a` (rating 0.9) carries 1.0 pu, a
/// loading of 1.0/0.9 = 1.1111; branch `b` (rating 0.7) carries the same
/// 1.0 pu, a loading of 1.0/0.7 = 1.4286. Both loadings sit strictly between
/// 1.0 (pickup) and 2.5 (instantaneous), so both trip on the inverse-time
/// curve alone, at different times: with the default `StandardInverse`
/// curve (A = 0.14, B = 0.02) and `time_multiplier = 0.1`,
/// `t = TM·A / (mᴮ − 1)` gives
///   branch a: m = 1.1111 → t ≈ 6.637 s
///   branch b: m = 1.4286 → t ≈ 1.956 s
/// a gap of about 4.68 s between them — wide enough that a narrow and a wide
/// coalescing window land on opposite sides of it deterministically.
fn two_trip_times_case() -> Case {
    let base = Base::new(100e6, 345e3).expect("a valid base");
    let buses = vec![
        Bus::slack("g0", 1.0)
            .with_real_limits(0.0, 10.0)
            .with_machine(4.0, 0.05),
        Bus::load("l0", -1.0, 0.0),
        Bus::load("l1", -1.0, 0.0),
    ];
    let branches = vec![
        Branch::new("a", 0, 1, 0.01, 0.1).with_rating(0.9),
        Branch::new("b", 0, 2, 0.01, 0.1).with_rating(0.7),
    ];

    Case::new("two-trip-times", base, buses, branches)
}

/// A window narrower than the ~4.68 s gap between the two trip times fires
/// only the earlier candidate; a window wider than the gap fires both. Either
/// way, the earliest candidate is never left out — it is what the window is
/// measured from.
#[test]
fn the_window_selects_which_candidates_fire() {
    let mut a = two_trip_times_case();
    let mut b = two_trip_times_case();

    // Narrower than the ~4.68 s gap: only branch b (the faster of the two,
    // at ≈ 1.956 s) falls inside 1.956 s + 0.5 s = 2.456 s.
    let narrow = Options {
        coalesce: 0.5,
        ..Options::default()
    };
    // Wider than the gap: branch a's ≈ 6.637 s falls inside
    // 1.956 s + 10 s = 11.956 s too.
    let wide = Options {
        coalesce: 10.0,
        ..Options::default()
    };

    let with_narrow = step(
        &mut a,
        &Protection::new(Settings::default(), Vec::new()),
        &Scheme::none(),
        &narrow,
    )
    .expect("solvable");
    let with_wide = step(
        &mut b,
        &Protection::new(Settings::default(), Vec::new()),
        &Scheme::none(),
        &wide,
    )
    .expect("solvable");

    assert_eq!(with_narrow.fired.len(), 1, "fired {:?}", with_narrow.fired);
    assert_eq!(with_narrow.fired[0].branch, 1, "branch b trips first");

    assert_eq!(with_wide.fired.len(), 2, "fired {:?}", with_wide.fired);
    let wide_branches: Vec<usize> = with_wide.fired.iter().map(|c| c.branch).collect();
    assert!(wide_branches.contains(&0) && wide_branches.contains(&1));

    // The earliest candidate fires under both windows — it is the anchor,
    // not something the window itself can exclude.
    assert!(with_narrow.fired.iter().any(|c| c.branch == 1));
    assert!(with_wide.fired.iter().any(|c| c.branch == 1));
}

/// Solving an island twice against different reference buses gives identical
/// flows — because dispatch balanced it first, so the reference absorbs
/// nothing.
///
/// This is the oracle-independent check. If it fails, the island arrived
/// unbalanced and the reference bus is silently generating, which shows up
/// later as a cascade that quietly stops propagating.
#[test]
fn flows_do_not_depend_on_the_reference_bus() {
    use bs_cascade::{Islands, Subcase};
    use bs_powerflow::solve_dc;

    let mut case = parallel_case(1.0);
    // Balance it explicitly so the property under test is the reference, not
    // the dispatch.
    case.buses[0].p_injection = 1.0;

    let islands = Islands::of(&case);
    let sub = Subcase::extract(&case, &islands.members[0]).expect("has a machine");
    let first = solve_dc(&sub.case).expect("solvable").flows;

    let mut swapped = sub.case.clone();
    swapped.buses[0].kind = bs_case::BusKind::Pv;
    swapped.buses[1].kind = bs_case::BusKind::Slack;
    let second = solve_dc(&swapped).expect("solvable").flows;

    for (index, (a, b)) in first.iter().zip(second.iter()).enumerate() {
        assert!((a - b).abs() < 1e-9, "branch {index}: {a} vs {b}");
    }
}

/// A separated network is stepped island by island, and a de-energized island
/// is reported rather than dropped.
#[test]
fn a_separated_network_steps_every_island() {
    let base = Base::new(100e6, 345e3).expect("a valid base");
    let mut case = Case::new(
        "separated",
        base,
        vec![
            Bus::slack("g0", 1.0)
                .with_real_limits(0.0, 3.0)
                .with_machine(4.0, 0.05),
            Bus::load("l0", -1.0, 0.0),
            Bus::load("l1", -1.0, 0.0),
        ],
        vec![
            Branch::new("a", 0, 1, 0.01, 0.1).with_rating(2.0),
            Branch::new("tie", 1, 2, 0.01, 0.1).with_rating(2.0),
        ],
    );
    case.branches[1].in_service = false;

    let got = step(
        &mut case,
        &Protection::new(Settings::default(), Vec::new()),
        &Scheme::none(),
        &Options::default(),
    )
    .expect("solvable");

    assert_eq!(got.islands.count(), 2);
    assert_eq!(got.responses.len(), 2);
    // The dead island has no machine and therefore no frequency model.
    assert!(got.responses[1].is_none());
}

/// Every other test runs `Scheme::none()`, so none of them exercise the
/// branch where a stage actually operates. This is the mirror case: a
/// residual dispatch cannot close drives the island's frequency below a
/// stage's threshold, and the shed that produces must land on the load
/// *before* the power flow solves, pro-rata across the island's PQ buses,
/// not just get reported after the fact.
#[test]
fn a_shed_reduces_load_before_the_power_flow_solves() {
    let base = Base::new(100e6, 345e3).expect("a valid base");
    let buses = vec![
        Bus::slack("g0", 1.0)
            .with_real_limits(0.0, 3.0)
            .with_machine(4.0, 0.05),
        // A load bigger than the slack's headroom, so dispatch cannot close
        // the whole gap and a residual imbalance is left for the frequency
        // model to see.
        Bus::load("l0", -4.0, 0.0),
    ];
    let branches = vec![Branch::new("a", 0, 1, 0.01, 0.1).with_rating(10.0)];
    let mut case = Case::new("heavy", base, buses, branches);

    // A threshold one hundredth of a hertz below nominal: any nonzero
    // imbalance crosses it on the very first integration step, so the test
    // does not depend on hand-solving the swing equation to know when.
    let scheme = Scheme {
        stages: vec![bs_frequency::Stage {
            at_hz: 59.99,
            shed_fraction: 0.2,
            delay: 0.0,
        }],
        floor_hz: 40.0,
        ceiling_hz: 70.0,
    };

    let got = step(
        &mut case,
        &Protection::new(Settings::default(), Vec::new()),
        &scheme,
        &Options::default(),
    )
    .expect("solvable");

    let response = got.responses[0].as_ref().expect("the island has a machine");
    assert!(
        !response.sheds.is_empty(),
        "a residual this large must trip the stage"
    );
    assert!((response.shed_fraction - 0.2).abs() < 1e-9);

    // 4.0 pu of load, twenty per cent shed, leaves 3.2 — applied to the one
    // PQ bus in the island, since it is the only one there is.
    assert!(
        (case.buses[1].p_injection - (-3.2)).abs() < 1e-9,
        "p_injection {}",
        case.buses[1].p_injection
    );

    // The shed's time is what fed the merged timeline in step 6 — this is
    // the oracle-independent check that shedding is on the same clock as
    // protection, not reported from a second one nothing else reads.
    assert!(got.at > 0.0);

    // The power flow itself has to have seen the shed, not just the case
    // afterward: with one radial branch carrying the whole load, the branch
    // flow is exactly the PQ bus's post-shed injection, 3.2 pu. If the shed
    // were applied after `solve_dc` instead of before it, this would still
    // read 4.0.
    assert!(
        (got.flows[0].abs() - 3.2).abs() < 1e-6,
        "flow {}",
        got.flows[0]
    );

    // The branch is rated 10.0, so 3.2 pu never comes close to tripping it —
    // this island's only timeline entry is the shed. `at` still comes back
    // positive and `fired` is still empty, which is the cross-source case
    // step 6's merge exists for: a shed alone can anchor the step's clock
    // with no protection candidate in the timeline at all.
    assert!(got.fired.is_empty(), "fired {:?}", got.fired);
}

/// Per-island real-power conservation, checked at every `step` call, driven
/// directly rather than through `simulate`.
///
/// Dispatch never sheds load — it only ever moves GENERATION within a unit's
/// own headroom (see `bs-cascade/src/dispatch.rs`'s own module doc) — so the
/// only thing on this whole path that is allowed to change an island's LOAD
/// at all is UFLS. The invariant this test pins down: for every single
/// `step` call, `load_before - load_after` must equal exactly what that
/// call's own responses report having shed — computed here independently,
/// via `response.shed_fraction * load_before`, the same formula
/// `bs-cascade/src/simulate.rs` uses internally, but written a second time
/// rather than trusting that module's own bookkeeping. Load must never
/// simply vanish, or appear, between one step and the next.
///
/// This is the invariant `Cascade::load_lost` is built on, and it had no
/// direct test anywhere before this one — `ufls_shedding_is_counted_in_load_lost`
/// and its siblings in `simulate.rs` only ever check the SUM at the end of a
/// whole run, which is exactly the kind of check a bug like "an island that
/// collapses while keeping its generator quietly drops its remaining load
/// from the total" can pass while still being wrong: the final sum can come
/// out looking plausible even when a specific step's own bookkeeping,
/// checked in isolation, would not. Checking every step directly, against
/// an independently written formula, is what closes that gap.
///
/// The scheme's single stage has no built-in "already fired" memory — see
/// `bs_frequency::ufls::respond`'s own module doc — so as long as a residual
/// imbalance remains, the SAME stage can and does keep firing on later
/// `step` calls too, each time against whatever load remains. That is
/// exactly why this loops several calls rather than checking only the
/// first: the invariant has to hold on every one of them, not just the step
/// that happens to be easiest to hand-derive.
///
/// What would make this fail: any change to `step`/`respond` that scales an
/// island's PQ injections by something other than exactly
/// `response.shed_fraction`, or that applies a shed more than once, or not
/// at all, for a stage `step` itself reports as having fired.
#[test]
fn real_power_is_conserved_at_every_step() {
    let base = Base::new(100e6, 345e3).expect("a valid base");
    let buses = vec![
        Bus::slack("g0", 1.0)
            .with_real_limits(0.0, 3.0)
            .with_machine(4.0, 0.05),
        Bus::load("l0", -10.0, 0.0),
    ];
    let branches = vec![Branch::new("a", 0, 1, 0.01, 0.1).with_rating(20.0)];
    let mut case = Case::new("heavy", base, buses, branches);

    let scheme = Scheme {
        stages: vec![bs_frequency::Stage {
            at_hz: 59.99,
            shed_fraction: 0.3,
            delay: 0.0,
        }],
        floor_hz: 57.0,
        ceiling_hz: 61.8,
    };

    // No protection at all: an overload or distance trip splitting the
    // island partway through is a real and separately tested scenario (see
    // `a_separated_network_steps_every_island`), but it is not what this
    // test is about, and letting one happen here would make a would-be
    // failure ambiguous between "the shed arithmetic is wrong" and "the
    // island split when this test didn't expect it to".
    let disabled = Settings {
        overcurrent: bs_protection::Overcurrent {
            pickup: f64::INFINITY,
            instantaneous: None,
            ..bs_protection::Overcurrent::default()
        },
        distance: bs_protection::Distance {
            zones: Vec::new(),
            breaker_time: 0.05,
        },
    };
    let protection = Protection::new(disabled, Vec::new());

    for call in 0..3 {
        use bs_cascade::{Balance, Islands};

        let before = Islands::of(&case);
        let load_before: Vec<f64> = before
            .members
            .iter()
            .map(|members| Balance::of(&case.buses, members).load)
            .collect();

        let got = step(&mut case, &protection, &scheme, &Options::default()).expect("solvable");

        assert_eq!(
            got.islands.count(),
            1,
            "call {call}: this fixture must never split — see the comment above"
        );

        let shed_this_call: f64 = got
            .responses
            .iter()
            .enumerate()
            .map(|(index, response)| {
                response.as_ref().map_or(0.0, |r| {
                    if r.shed_fraction > 0.0 {
                        r.shed_fraction * load_before[index]
                    } else {
                        0.0
                    }
                })
            })
            .sum();

        let after = Islands::of(&case);
        let load_after: Vec<f64> = after
            .members
            .iter()
            .map(|members| Balance::of(&case.buses, members).load)
            .collect();

        assert!(
            (load_before[0] - load_after[0] - shed_this_call).abs() < 1e-9,
            "call {call}: load_before {} - load_after {} != shed {} \
             (difference {})",
            load_before[0],
            load_after[0],
            shed_this_call,
            load_before[0] - load_after[0] - shed_this_call
        );
    }
}
