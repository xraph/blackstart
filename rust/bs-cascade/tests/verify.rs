use bs_cascade::{AcVerdict, Islands, verify_islands};
use bs_case::{Branch, Bus, Case};
use bs_powerflow::ac::{Options as AcOptions, VoltageLimits};
use bs_quantity::Base;

/// `Base` lives in `bs_quantity`, not `bs_case` — matching
/// `rust/bs-cascade/tests/simulate.rs`, which already gets this right.
///
/// A generator and a load on a short, comfortably rated line.
fn healthy_case() -> Case {
    let base = Base::new(100e6, 345e3).expect("a valid base");

    Case::new(
        "healthy",
        base,
        vec![
            Bus::slack("g0", 1.0)
                .with_real_limits(0.0, 3.0)
                .with_machine(4.0, 0.05),
            Bus::load("l0", -0.5, -0.1),
        ],
        vec![Branch::new("a", 0, 1, 0.01, 0.1).with_rating(2.0)],
    )
}

/// A load far beyond what the network can deliver. The AC solve has no
/// steady state to find, which is the condition `Insolvable` exists to name.
fn unsolvable_case() -> Case {
    let base = Base::new(100e6, 345e3).expect("a valid base");

    Case::new(
        "unsolvable",
        base,
        vec![
            Bus::slack("g0", 1.0)
                .with_real_limits(0.0, 50.0)
                .with_machine(4.0, 0.05),
            Bus::load("l0", -40.0, -20.0),
        ],
        // A high-impedance line cannot deliver forty per unit at any voltage.
        vec![Branch::new("a", 0, 1, 0.05, 0.5).with_rating(50.0)],
    )
}

/// One live island and one de-energized one, the tie between them open.
fn separated_case() -> Case {
    let base = Base::new(100e6, 345e3).expect("a valid base");
    let mut case = Case::new(
        "separated",
        base,
        vec![
            Bus::slack("g0", 1.0)
                .with_real_limits(0.0, 3.0)
                .with_machine(4.0, 0.05),
            Bus::load("l0", -0.5, -0.1),
            Bus::load("l1", -0.5, -0.1),
        ],
        vec![
            Branch::new("a", 0, 1, 0.01, 0.1).with_rating(2.0),
            Branch::new("tie", 1, 2, 0.01, 0.1).with_rating(2.0),
        ],
    );
    case.branches[1].in_service = false;

    case
}

/// A comfortable network verifies secure.
///
/// Fails if `verify_islands` ever reports a lightly loaded, well-rated
/// island as anything other than `Secure` — e.g. a sign error in the
/// thermal-loading check, or a voltage-band comparison that rejects a
/// converged flat-ish solution.
#[test]
fn a_healthy_island_is_secure() {
    let case = healthy_case();
    let reports = verify_islands(
        &case,
        &Islands::of(&case),
        VoltageLimits::default(),
        AcOptions::default(),
    );

    assert_eq!(reports.len(), 1);
    assert_eq!(reports[0].verdict, AcVerdict::Secure);
    assert_eq!(reports[0].bus_count, case.n_bus());
}

/// An island whose AC solve diverges is `Insolvable`, not secure.
///
/// This is the most serious verdict, not the least. Reporting a network that
/// may have no steady state at all as "no violations found" inverts it.
///
/// Fails if divergence is ever mapped to `Secure` (a `.unwrap_or(Secure)`-
/// shaped bug) or surfaced as a panic/`Err` instead of this verdict.
#[test]
fn a_diverging_island_is_insolvable_not_secure() {
    let case = unsolvable_case();
    let reports = verify_islands(
        &case,
        &Islands::of(&case),
        VoltageLimits::default(),
        AcOptions::default(),
    );

    assert_eq!(reports[0].verdict, AcVerdict::Insolvable);
}

/// A de-energized island is reported with its bus count and its lost load,
/// never dropped from the list.
///
/// Fails if `verify_islands` filters out islands with no machine
/// (`reports.len()` would be 1, not 2) or reports a de-energized island with
/// `bus_count == 0`.
#[test]
fn a_dead_island_still_gets_a_report() {
    let case = separated_case();
    let reports = verify_islands(
        &case,
        &Islands::of(&case),
        VoltageLimits::default(),
        AcOptions::default(),
    );

    assert_eq!(reports.len(), 2);
    assert!(reports[1].bus_count > 0);
}

/// The de-energized island's own verdict is `Insolvable`, not `Secure` — it
/// has no machine to hold a reference angle, so there is no AC solution to
/// have found, and "no violations found" would be a claim about a voltage
/// profile nobody produced.
///
/// Fails if a de-energized island is ever reported `Secure` by default (e.g.
/// an `AcVerdict::default()`-shaped bug, or a verdict left at whatever the
/// enum's first variant happens to be).
#[test]
fn a_dead_island_is_insolvable() {
    let case = separated_case();
    let reports = verify_islands(
        &case,
        &Islands::of(&case),
        VoltageLimits::default(),
        AcOptions::default(),
    );

    assert_eq!(reports[1].verdict, AcVerdict::Insolvable);
    assert_eq!(reports[1].bus_count, 1);
}

/// A PV bus with real, explicit generation exceeding the one load on its
/// island — the one fixture in this file where `generation` is not left at
/// a slack bus's default zero.
fn surplus_case() -> Case {
    let base = Base::new(100e6, 345e3).expect("a valid base");

    Case::new(
        "surplus",
        base,
        vec![
            Bus::slack("g0", 1.0).with_real_limits(0.0, 5.0),
            Bus::generator("g1", 2.0, 1.0).with_real_limits(0.0, 5.0),
            Bus::load("l0", -0.5, 0.0),
        ],
        vec![
            Branch::new("a", 0, 1, 0.01, 0.1).with_rating(10.0),
            Branch::new("b", 1, 2, 0.01, 0.1).with_rating(10.0),
        ],
    )
}

/// `IslandReport::balance` is GENERATION MINUS LOAD, negative for a
/// shortfall — the opposite sign from `Balance::gap` (this crate's own
/// internal dispatch convention, load minus generation), and the sign
/// `IslandState.balance_w`'s own proto comment names as the authority both
/// sides of the wire must agree with. `separated_case`'s dead island (`l1`
/// alone, 0.5 pu of load and no generation at all) is the plainest possible
/// shortfall: `generation - load = 0.0 - 0.5 = -0.5`.
///
/// What would make this fail: `verify_one` computing `balance` from
/// `Balance::of(..).gap` directly (load minus generation) instead of negating
/// it — this fixture's reported balance would come back `+0.5` instead of
/// `-0.5`.
#[test]
fn a_short_islands_balance_is_negative() {
    let case = separated_case();
    let reports = verify_islands(
        &case,
        &Islands::of(&case),
        VoltageLimits::default(),
        AcOptions::default(),
    );

    assert!(
        (reports[1].balance - -0.5).abs() < 1e-9,
        "balance {}, want -0.5 pu (generation 0.0 - load 0.5)",
        reports[1].balance
    );
}

/// The other sign: an island with real surplus generation reports a
/// POSITIVE balance. `surplus_case`'s two PV/slack buses sum to
/// `0.0 + 2.0 = 2.0` pu of generation against `0.5` pu of load:
/// `generation - load = 2.0 - 0.5 = 1.5`.
///
/// Pinning both signs, not just the shortfall one above, is what rules out a
/// fix that merely swapped which branch of an `if` was negated rather than
/// actually reversing the convention.
#[test]
fn a_surplus_islands_balance_is_positive() {
    let case = surplus_case();
    let reports = verify_islands(
        &case,
        &Islands::of(&case),
        VoltageLimits::default(),
        AcOptions::default(),
    );

    assert_eq!(reports.len(), 1);
    assert!(
        (reports[0].balance - 1.5).abs() < 1e-9,
        "balance {}, want 1.5 pu (generation 2.0 - load 0.5)",
        reports[0].balance
    );
}

/// Islands come back in the same order `Islands::of` gives them — the live
/// one (containing bus 0, the lowest member) first.
///
/// Fails if `verify_islands` reorders islands (e.g. sorting by severity or
/// by bus count) rather than preserving `Islands::members`' own order.
#[test]
fn islands_come_back_in_canonical_order() {
    let case = separated_case();
    let islands = Islands::of(&case);
    let reports = verify_islands(
        &case,
        &islands,
        VoltageLimits::default(),
        AcOptions::default(),
    );

    for (report, members) in reports.iter().zip(islands.members.iter()) {
        assert_eq!(&report.members, members);
    }
}
