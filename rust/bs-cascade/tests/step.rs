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

/// Everything inside the coalescing window fires together; everything outside
/// waits for the next step.
#[test]
fn the_window_selects_which_candidates_fire() {
    let mut case = parallel_case(0.6);
    case.branches[0].in_service = false;

    let wide = Options {
        coalesce: 100.0,
        ..Options::default()
    };
    let narrow = Options {
        coalesce: 0.0,
        ..Options::default()
    };

    let mut a = case.clone();
    let mut b = case;

    let with_wide = step(
        &mut a,
        &Protection::new(Settings::default(), Vec::new()),
        &Scheme::none(),
        &wide,
    )
    .expect("solvable");
    let with_narrow = step(
        &mut b,
        &Protection::new(Settings::default(), Vec::new()),
        &Scheme::none(),
        &narrow,
    )
    .expect("solvable");

    assert!(with_wide.fired.len() >= with_narrow.fired.len());
    assert!(!with_narrow.fired.is_empty(), "the earliest always fires");
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
}
