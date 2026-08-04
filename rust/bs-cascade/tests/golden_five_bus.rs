//! A hand-computed five-bus cascade, end to end.
//!
//! Every number below is derived from the network and the relay/shedding
//! settings BEFORE the simulator was ever run against this case — see the
//! comments at each assertion for the arithmetic. Running the simulator and
//! copying its output into the assertions would test nothing: it would lock
//! in whatever the implementation currently does, bugs included, rather than
//! what the network actually requires it to do.
//!
//! # The network
//!
//! ```text
//!            "a"            "b"
//!     l0 (1) ---- g0 (0) ---- l1 (2)
//!                  |  |
//!             "tie1"  "tie2"     (both 0-3, a double-circuit tie)
//!                  \  /
//!                 l2 (3)
//!                   |
//!                  "d"
//!                   |
//!                 l3 (4)
//! ```
//!
//! One generator, `g0` — `p_max = 3.0`, `H = 4.0`, `R = 0.05`, exactly the
//! brief's spec — with two loads (`l0`, `l1`) reachable only through it, and
//! two more loads (`l2`, `l3`) reachable only through a double-circuit tie.
//! Every load is 1.0 pu; every branch has `r = 0.01`, `x = 0.1`. `"a"`, `"b"`
//! and `"d"` are rated 2.0 (comfortably above anything that reaches them);
//! `"tie1"` and `"tie2"` are rated 1.0 each, so together the healthy pair
//! carries `l2 + l3 = 2.0` at 1.0 pu apiece — right at pickup, not over it.
//!
//! # Why a double-circuit tie
//!
//! With one generator and no other source, splitting load buses off the
//! network can only ever *reduce* what the generator has left to serve — it
//! can never create a fresh deficit for the surviving island. So the only way
//! for `g0`'s own island to end up shedding, while the network is still one
//! piece, is for the *whole* network's demand to already exceed `p_max`
//! before the tie is lost. That is exactly what this case does: total demand
//! is 4.0 against `p_max = 3.0`, a deficit that exists from the moment the
//! initiating outage opens `tie1`, while the network is still connected
//! through `tie2` alone.
//!
//! # The arithmetic
//!
//! **Step 0 (`t = 0`, [`open_initiating`]).** `tie1` opens. `tie1` and `tie2`
//! are parallel — the network is still one connected piece through `tie2` —
//! so this does not split anything; it only removes half the tie's capacity.
//!
//! **Step 1.** The network is still one island (all 5 buses), so dispatch and
//! shedding run against the *whole* network:
//! - Dispatch: gap `= 4.0 - 0 = 4.0` (`g0` starts at zero output), covered up
//!   to `p_max = 3.0` (headroom `3.0 - 0 = 3.0`, and `4.0 > 3.0` so the whole
//!   headroom is used). `g0.p_injection` becomes exactly `3.0`. Residual
//!   (the frequency model's imbalance) `= 4.0 - 3.0 = 1.0`.
//! - Frequency: with `H = 4.0`, `damping = 1.0` (default) and
//!   `inverse_droop = (p_max / R) / p_max = 1 / R = 20.0` (one machine, so the
//!   generation-weighted reduction is just its own numbers), the initial rate
//!   of change is `-imbalance / (2H) = -1.0 / 8.0 = -0.125` pu/s, i.e.
//!   `-7.5` Hz/s. The one shedding stage sits at `59.99` Hz (`0.01` Hz below
//!   nominal) with no hold delay — the same technique
//!   `tests/simulate.rs::ufls_shedding_is_counted_in_load_lost` uses: at
//!   `-7.5` Hz/s, the deviation after one `0.01` s integration step is about
//!   `-0.075` Hz, seven and a half times past the `-0.01` Hz threshold — so
//!   the stage crosses on the very first integration sample, deterministically,
//!   without needing to hand-integrate the swing equation to know exactly
//!   when. It sheds `0.25 x 4.0 = 1.0` pu — exactly the residual — so the
//!   network settles at nominal frequency with nothing left over. Every PQ
//!   bus is reduced by the same 25%: `l0 = l1 = l2 = l3 = 0.75` pu from here
//!   on.
//! - Protection: with the loads just reduced, `tie2` alone now carries
//!   `l2 + l3 = 0.75 + 0.75 = 1.5` pu against its 1.0 pu rating — a loading of
//!   1.5, between pickup (1.0) and the instantaneous element (2.5), so it
//!   trips on the inverse-time curve: `t = TM x A / (mᴮ - 1)` with the default
//!   `StandardInverse` curve (`A = 0.14`, `B = 0.02`, `TM = 0.1`) and `m =
//!   1.5` gives `t = 0.014 / (1.5^0.02 - 1) ≈ 0.014 / 0.0081423 ≈ 1.7194` s —
//!   far outside the default 0.1 s coalescing window around the shed's
//!   ~0.01 s crossing, so `tie2` does **not** fire this step. Distance
//!   protection never enters into it: at `x = 0.1`, even zone 3's reach is
//!   `1.5 x 0.1 = 0.15`, requiring an apparent impedance below 0.15, i.e. a
//!   flow above `1/0.15 ≈ 6.67` pu — far more than the 1.5 pu actually
//!   flowing.
//!
//!   This step's only loggable effect is the shed.
//!
//! **Step 2.** Nothing has changed load- or generation-wise
//! (`g0` still at 3.0, loads still at 0.75 each), so dispatch finds
//! `gap = 3.0 - 3.0 = 0` and the frequency model sees zero imbalance — no
//! further shedding. `tie2`'s flow is unchanged at 1.5 pu, so its trip
//! candidate reappears with the same ~1.7194 s, and this time it is the
//! *only* candidate in the timeline, so it fires on its own. Firing it opens
//! both tie circuits, and the network splits into two islands: `{g0, l0, l1}`
//! (bus 0, 1, 2) and `{l2, l3}` (bus 3, 4) — exactly the case's "two load
//! buses inside the generating island, two load buses across the tie".
//!
//! **Step 3 and settling.** The generating island now serves only
//! `l0 + l1 = 0.75 + 0.75 = 1.5` against `g0`'s carried-over 3.0 pu output —
//! a 1.5 pu *surplus* — so dispatch backs `g0` down to 1.5 exactly, closing
//! the gap with nothing left for the frequency model to see. The dead island
//! `{l2, l3}` has no machine, so it gets no frequency response and its 0.75 pu
//! loads simply sit there, unserved. Step 3 finds nothing left to trip or
//! shed, so the cascade settles without logging anything further.
//!
//! **Totals.** Shed in step 1: `0.25 x 4.0 = 1.0` pu (the original, pre-shed
//! total — [`simulate`]'s `shed_total` is computed against `served_before`,
//! the load as of the top of that step, not the reduced figure). Stranded in
//! the dead island at the end: `l2 + l3 = 0.75 + 0.75 = 1.5` pu.
//! `load_lost = 1.0 + 1.5 = 2.5` pu. Cross-check against the original total:
//! `4.0 = 1.5 (served by g0) + 2.5 (lost)`. ✓
//!
//! # What is asserted, and what is not
//!
//! The exact event sequence (count, kind, order, branch, island), the two
//! `detail` strings (loading percentage and shed fraction, both exact,
//! closed-form numbers), `outcome`, `islands.count()`, `steps`, and
//! `load_lost` are all asserted exactly or to a tight tolerance — every one of
//! them is derived above from the ratings and the relay/shedding settings
//! alone.
//!
//! The individual event `at` timestamps are **not** asserted to high
//! precision. The shed's own crossing time is the output of a fixed-step RK4
//! integration of the swing equation, not a closed form — the *same*
//! reasoning `tests/simulate.rs` already documents for why it does not
//! hand-solve the swing equation. What the relay-curve arithmetic above does
//! give exactly is the trip's *own* interval (`≈ 1.7194` s), so `tie2`'s
//! logged `at` is asserted against a loose window around
//! `0.01 + 1.7194 ≈ 1.7294` s that comfortably contains any plausible
//! crossing time, without pretending the crossing time itself was hand-solved.

use bs_cascade::{CascadeOutcome, EventKind, Limits, Options, Verification, simulate};
use bs_case::{Branch, Bus, Case};
use bs_frequency::{Scheme, Stage};
use bs_protection::{Protection, Settings};
use bs_quantity::Base;

/// The five-bus network described in the module docs.
fn five_bus_case() -> Case {
    let base = Base::new(100e6, 345e3).expect("a valid base");

    let buses = vec![
        Bus::slack("g0", 1.0)
            .with_real_limits(0.0, 3.0)
            .with_machine(4.0, 0.05),
        Bus::load("l0", -1.0, 0.0),
        Bus::load("l1", -1.0, 0.0),
        Bus::load("l2", -1.0, 0.0),
        Bus::load("l3", -1.0, 0.0),
    ];

    let branches = vec![
        Branch::new("a", 0, 1, 0.01, 0.1).with_rating(2.0),
        Branch::new("b", 0, 2, 0.01, 0.1).with_rating(2.0),
        Branch::new("tie1", 0, 3, 0.01, 0.1).with_rating(1.0),
        Branch::new("tie2", 0, 3, 0.01, 0.1).with_rating(1.0),
        Branch::new("d", 3, 4, 0.01, 0.1).with_rating(2.0),
    ];

    Case::new("five-bus", base, buses, branches)
}

/// One shedding stage, set just below nominal with no hold delay so that any
/// nonzero imbalance crosses it on the very first integration sample — see
/// the module docs.
fn one_stage_scheme() -> Scheme {
    Scheme {
        stages: vec![Stage {
            at_hz: 59.99,
            shed_fraction: 0.25,
            delay: 0.0,
        }],
        floor_hz: 40.0,
        ceiling_hz: 70.0,
    }
}

/// The hand-derived cascade: overload, inverse-time trip, split, one UFLS
/// stage, settle. See the module docs for the full arithmetic.
#[test]
fn the_five_bus_cascade_matches_the_hand_derivation() {
    let case = five_bus_case();

    let got = simulate(
        &case,
        &[2], // "tie1"
        &Protection::new(Settings::default(), Vec::new()),
        &one_stage_scheme(),
        &Options::default(),
        &Limits::default(),
        &Verification::default(),
    )
    .expect("solvable");

    // Exactly four events: the initiating trip, one shed, one further trip,
    // one island split. A propagation that fired an extra branch, missed the
    // shed, or split into more than two pieces would change this count.
    assert_eq!(
        got.events.len(),
        4,
        "events: {:#?}",
        got.events
            .iter()
            .map(|e| (e.kind, &e.element, e.at))
            .collect::<Vec<_>>()
    );

    let kinds: Vec<EventKind> = got.events.iter().map(|e| e.kind).collect();
    assert_eq!(
        kinds,
        vec![
            EventKind::Trip,
            EventKind::Shed,
            EventKind::Trip,
            EventKind::Island,
        ],
        "event kinds out of order: {kinds:?}"
    );

    let elements: Vec<&str> = got.events.iter().map(|e| e.element.as_str()).collect();
    assert_eq!(
        elements,
        vec!["tie1", "g0", "tie2", "tie2"],
        "wrong branch/bus named at each step: {elements:?}"
    );

    // Every logged event belongs to island 0 except the split itself, which
    // by construction cannot be attributed to just one side of it.
    let islands: Vec<Option<usize>> = got.events.iter().map(|e| e.island).collect();
    assert_eq!(islands, vec![Some(0), Some(0), Some(0), None]);

    // The initiating outage is logged at exactly t = 0.
    assert!(
        (got.events[0].at - 0.0).abs() < 1e-12,
        "at {}",
        got.events[0].at
    );
    assert_eq!(got.events[0].detail, "initiating outage");

    // One stage, exactly 25% — the closed-form fraction chosen to close the
    // 1.0 pu residual against the 4.0 pu pre-shed total exactly.
    assert_eq!(got.events[1].detail, "stage 1 · 25%");

    // tie2's loading is exactly 1.5 (1.5 pu flow over a 1.0 pu rating), so
    // its detail string is exact regardless of any floating-point noise in
    // the flow itself.
    assert_eq!(got.events[2].detail, "150% · oc");

    // tie2's own relay-curve interval is closed-form: t = TM·A / (mᴮ - 1) with
    // A = 0.14, B = 0.02, TM = 0.1, m = 1.5 → t ≈ 1.7194 s. Its logged `at` is
    // that plus the shed's own crossing time (step 1's `at`), which is not
    // hand-solved exactly — see the module docs — so this is checked against
    // a window wide enough to hold any plausible crossing time (the horizon
    // is 60 s) without pretending false precision.
    let relay_interval = 0.1 * 0.14 / (1.5_f64.powf(0.02) - 1.0);
    assert!(
        (relay_interval - 1.7194).abs() < 1e-3,
        "hand-derived relay interval {relay_interval} does not match the ~1.7194s written above"
    );
    let tie2_at = got.events[2].at;
    assert!(
        tie2_at > relay_interval && tie2_at < relay_interval + 1.0,
        "tie2 trip at {tie2_at}s, expected shortly after the {relay_interval}s relay interval"
    );
    // The island split is coalesced into the same step, so it shares tie2's
    // own `at` exactly.
    assert!(
        (got.events[3].at - tie2_at).abs() < 1e-12,
        "island split at {}, tie2 trip at {tie2_at}",
        got.events[3].at
    );

    // Two steps counted: step 1 (shed only) and step 2 (trip only). Step 3
    // finds nothing left to do and is not counted — see `Cascade::steps`.
    assert_eq!(got.steps, 2);

    assert_eq!(got.outcome, CascadeOutcome::Settled);
    assert_eq!(got.islands.count(), 2);

    // 1.0 pu shed in step 1, plus the 0.75 + 0.75 = 1.5 pu stranded in the
    // dead island at the end: 1.0 + 1.5 = 2.5 pu. Cross-checked against the
    // original 4.0 pu total: 4.0 = 1.5 (served by g0) + 2.5 (lost).
    assert!(
        (got.load_lost - 2.5).abs() < 1e-9,
        "load_lost {}",
        got.load_lost
    );

    // The generating island keeps its two loads; the dead island has the
    // other two. The generating island (containing bus 0) sorts first.
    assert_eq!(got.islands.members[0], vec![0, 1, 2]);
    assert_eq!(got.islands.members[1], vec![3, 4]);
}
