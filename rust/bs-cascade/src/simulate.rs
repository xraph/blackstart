//! The cascade loop: repeated stepping from an initiating outage to a
//! settled, truncated, or collapsed end state.
//!
//! [`step`] answers what happens in one instant. A cascade is a sequence of
//! those instants, so this module owns exactly the things a single step
//! cannot: opening the branches that started it, deciding when to stop, and
//! turning the accumulated changes into the time-ordered log an operator
//! reads back.
//!
//! Every event this module emits carries an absolute time, and every event
//! fired within one call to [`step`] is reported at the SAME instant — that
//! call's own coalesced `at` — rather than at its own, finer time within the
//! coalescing window. That is not a loss of resolution so much as taking
//! coalescing at its word: events inside the window are already being
//! treated as one reportable moment. Reporting them at their individual
//! sub-times instead would let one step's tail event land after the *next*
//! step's own opening event, because each step's protection and shedding
//! times are measured fresh from that step's own start, not from the
//! cascade's clock. That is exactly the non-monotonic log the operator
//! console's schema refuses to render. Collapsing every event in a step to
//! that step's own `at` is what keeps the ordering invariant true by
//! construction rather than by hoping the numbers happen to line up.
//!
//! Advisory only. See `docs/SAFETY.md`.

use bs_case::{Bus, Case};
use bs_frequency::{IslandCondition, Response, Scheme};
use bs_powerflow::ac::{Options as AcOptions, VoltageLimits};
use bs_protection::{Protection, TripCandidate, TripCause};

use crate::dispatch::Balance;
use crate::island::{Islands, Subcase};
use crate::step::{Options, Outcome, StepError, step};
use crate::verify::{IslandReport, verify_islands};

/// The five event kinds the operator console's schema recognizes.
///
/// The cascade loop only ever produces `Trip`, `Shed`, and `Island` —
/// `Overload` and `Stabilise` belong to other stages of the pipeline (a
/// contingency screen, and a settled-island summary). The enum names all
/// five here anyway, because the console's schema is the authority on what
/// an event log may contain, and a `bs-cascade`-local subset of it would
/// invite a second, silently different definition of the same five names.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum EventKind {
    /// A branch found to be past its rating. Not emitted by the cascade loop.
    Overload,
    /// A branch's protection operated and it left service.
    Trip,
    /// The network fell apart into more pieces than it had a moment ago.
    Island,
    /// Under-frequency load shedding operated.
    Shed,
    /// An island settled inside its survival band. Not emitted by the
    /// cascade loop.
    Stabilise,
}

/// One entry in the cascade's event log.
#[derive(Debug, Clone, PartialEq)]
pub struct Event {
    /// Seconds from the initiating outage. Non-decreasing across the whole
    /// log — see the module docs for why that is load-bearing, not
    /// cosmetic.
    pub at: f64,
    /// What kind of thing happened.
    pub kind: EventKind,
    /// The branch or bus `id` from the case. An operator names lines and
    /// buses, not their array indices.
    pub element: String,
    /// States the cause: a loading percentage and the protection element
    /// that saw it, a distance zone, or a shedding stage and fraction.
    pub detail: String,
    /// Which island this event belongs to, where that is meaningful.
    ///
    /// `Trip` and `Shed` events always carry one — including an initiating
    /// outage's own `Trip` events, which are resolved against the partition
    /// left once every initiating branch is already open, not against a
    /// partition computed before any of them opened. `None` only for an
    /// `Island` event: a single step can split one island into more than
    /// one new piece at once, and there is no cheap way to say the split
    /// itself belongs to just one of them.
    pub island: Option<usize>,
}

/// How a cascade ended.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum CascadeOutcome {
    /// A step found nothing left to trip or shed: the network reached a
    /// steady state within the limits given.
    Settled,
    /// The step or time budget ran out before the cascade reached a steady
    /// state.
    ///
    /// Never reported as `Settled` — an interrupted cascade's last known
    /// state says nothing about whether it would have gone on to settle,
    /// trip further, or collapse, and `Settled` claims the first of those
    /// three with no evidence for it. This is the same reasoning that puts
    /// `Verdict::Insolvable` ahead of `Secure` elsewhere in this codebase:
    /// an unresolved run outranks every resolved one, because a resolved
    /// verdict is a claim the run cannot back up.
    Truncated {
        /// Steps actually taken before the budget ran out.
        steps: usize,
    },
    /// Every island still carrying load is either de-energized or has left
    /// its survival band with the shedding scheme exhausted.
    Collapsed,
}

/// How much cascade a caller is willing to run before giving up on it
/// settling.
#[derive(Debug, Clone, Copy, PartialEq)]
pub struct Limits {
    /// Steps before the run is truncated.
    pub max_steps: usize,
    /// Simulated seconds before the run is truncated.
    pub horizon: f64,
}

impl Default for Limits {
    fn default() -> Self {
        Self {
            max_steps: 100,
            horizon: 3600.0,
        }
    }
}

/// How `simulate` should verify the islands it ends with — see
/// [`verify_islands`].
///
/// Bundled into one parameter, alongside [`Limits`]' own "how much cascade to
/// run", rather than as two more positional arguments to [`simulate`]: a
/// caller passes one policy object for "how to verify" and a future
/// verification-only tunable is a field added here, not another parameter
/// threaded through every call site again.
///
/// [`Default`] exists for convenience — the same convenience [`Limits`]'
/// `Default` already offers — but it is opt-in at the *call site*, not
/// silently assumed inside [`simulate`]. `VoltageLimits::default` documents
/// the acceptable voltage band as a utility's policy choice this crate has no
/// standing to assert on a caller's behalf; a caller that has its own band
/// passes it here instead.
#[derive(Debug, Clone, Copy, PartialEq, Default)]
pub struct Verification {
    /// The voltage band an AC solution is checked against.
    pub limits: VoltageLimits,
    /// Tunables for the AC solver itself.
    pub options: AcOptions,
}

/// A finished, or truncated, cascade.
#[derive(Debug, Clone, PartialEq)]
pub struct Cascade {
    /// How it ended.
    pub outcome: CascadeOutcome,
    /// Every event, in non-decreasing `at` order.
    pub events: Vec<Event>,
    /// The islands the case had settled, been truncated, or collapsed into.
    pub islands: Islands,
    /// Load lost: the load sitting in a de-energized island at the end, plus
    /// every fraction UFLS shed along the way.
    ///
    /// Computed once, here, from the final case state and a running tally
    /// kept during the loop — not incrementally recomputed at each step and
    /// folded into a second running total, which is how the two versions
    /// drift apart. See [`simulate`] for where each half comes from.
    pub load_lost: f64,
    /// Steps that produced a trip or a shed. A step that found nothing to
    /// do does not count — it is the evidence the cascade is over, not a
    /// step of it.
    pub steps: usize,
    /// Simulated seconds elapsed.
    pub elapsed: f64,
    /// A full AC verification of every island the cascade ended with, in
    /// canonical order.
    ///
    /// The loop above runs entirely in the linearised DC model — real
    /// voltages are not what makes a cascade propagate fast enough to
    /// simulate. This is computed once, after the loop, against the final
    /// islands: DC cannot see a voltage violation at all, and it cannot see
    /// a network with no steady state, so neither question has an answer
    /// until now. See [`Verification`] for the voltage band and AC solver
    /// options this verification runs under.
    ///
    /// Each report's `condition` is the MOST SEVERE frequency condition the
    /// cascade's own history ever observed for that island — not merely
    /// its condition at the final instant. An island that shed a quarter of
    /// its load and then settled `Stable` reports `Shedding`: the shed is
    /// the single most important fact about it, and a snapshot of the last
    /// step alone would discard it. See [`simulate`]'s body for the
    /// severity order and how a final island is matched back to what the
    /// loop observed, and why `None` is a real, distinct answer rather than
    /// a placeholder for "nobody checked".
    pub reports: Vec<IslandReport>,
}

/// One bus's frequency history across a whole cascade, reduced from
/// however many `step` calls observed it down to the single set of numbers
/// [`IslandReport`] wants.
///
/// Every field but `settling_hz` is a maximum-severity accumulation across
/// the run, matching how `condition` already worked before this struct
/// existed: `respond` (in `bs-frequency`) starts each call fresh from rest,
/// so nothing inside a single `step` call has the whole run's history to
/// draw on, and accumulating it is this caller's job. `settling_hz` is the
/// one deliberate exception — see its own field doc.
#[derive(Debug, Clone, Copy, Default)]
struct BusHistory {
    /// Peak severity — see `severity` below.
    condition: Option<IslandCondition>,
    /// Lowest absolute frequency (hertz, not a deviation) reached across
    /// every step this bus's island was observed in.
    nadir_hz: Option<f64>,
    /// Largest-magnitude signed rate of change (hertz per second) observed
    /// across every step.
    rocof_hz_s: Option<f64>,
    /// The absolute frequency (hertz) at the LAST step this bus's island had
    /// a response — overwritten every observation, not compared against the
    /// existing value, because a settling frequency is a final-instant
    /// quantity, not an extremum.
    settling_hz: Option<f64>,
    /// Per-unit load shed, summed across every step this bus's island shed
    /// anything.
    shed: f64,
}

/// Runs a cascade from an initiating outage to a settled, truncated, or
/// collapsed end state.
///
/// Every initiating branch opens at `t = 0`, simultaneously — a real
/// initiating event (a storm cell, a common corridor) does not stagger
/// itself to be convenient for this loop. From there, [`step`] is called
/// until one of three things is true: every island still carrying load has
/// died (`Collapsed`); the step found nothing left to trip or shed
/// (`Settled`); or the step or time budget is exhausted (`Truncated`). The
/// collapse check runs first, on purpose — see the loop body for why a step
/// that fires nothing can still be the step that reveals the network is
/// gone.
///
/// # Panics
///
/// Panics if `initiating` names a branch index the case does not have. That
/// is a caller mistake worth naming loudly: a silent no-op would report an
/// outage that never happened as though it had been simulated.
///
/// # Errors
///
/// Returns whatever [`StepError`] the underlying [`step`] returns, unchanged.
pub fn simulate(
    case: &Case,
    initiating: &[usize],
    protection: &Protection,
    scheme: &Scheme,
    options: &Options,
    limits: &Limits,
    verification: &Verification,
) -> Result<Cascade, StepError> {
    let mut case = case.clone();
    let (mut events, mut island_count) = open_initiating(&mut case, initiating);

    let mut elapsed = 0.0;
    let mut steps = 0;
    let mut shed_total = 0.0;

    // Nominal frequency does not change mid-run — `options` is the same
    // `&Options` handed to every `step` call below — so it is hoisted here
    // rather than re-read from `options.defaults.nominal_hz` at every
    // observation.
    let nominal = options.defaults.nominal_hz;

    // Every frequency figure any step ever observed for each bus, reduced to
    // the single number `IslandReport` wants for each — see `BusHistory` for
    // which of the four is a peak-severity maximum and which (settling) is
    // deliberately a last-write instead. `Default` gives every bus `None`
    // condition/nadir/rocof/settling and a `0.0` shed until some step
    // observes a machine on that bus's island.
    let mut bus_history: Vec<BusHistory> = vec![BusHistory::default(); case.n_bus()];

    let outcome = loop {
        if steps >= limits.max_steps || elapsed >= limits.horizon {
            break CascadeOutcome::Truncated { steps };
        }

        // The served load per island as of the instant `step` is about to
        // see it, captured before the call because `step` mutates `case` in
        // place — dispatch and any UFLS shed — and there is no way to
        // recover a pre-shed load from the post-shed injections afterward.
        let before = Islands::of(&case);
        let served_before: Vec<f64> = before
            .members
            .iter()
            .map(|members| Balance::of(&case.buses, members).load)
            .collect();

        let out = step(&mut case, protection, scheme, options)?;

        // Sticky, not overwritten: an island that sheds once and settles
        // `Stable` a moment later must not have that shed erased from its
        // record. `respond` (in `bs-frequency`) is deliberately memory-less
        // per call — each step's integration starts fresh from rest against
        // that step's own dispatch and topology, which is correct, since a
        // step's trajectory has no business inheriting a *different* step's
        // imbalance. That is exactly why severity has to be accumulated
        // here, by the caller, instead: nothing inside a single `step` call
        // has the whole run's history to draw on. Keyed by bus, not island
        // identity — see the end-of-run comment below for why.
        //
        // Unconditional — every step that produced a response updates
        // `bus_history`, including one whose own `is_collapsed` check
        // (below) is about to end the loop before `record_step` ever runs.
        // That is deliberate for `shed` in particular: a step's shed already
        // happened to `case` — `step` scaled the island's PQ injections
        // before returning — whether or not this loop goes on to log it as
        // an event, and `bus_history` is where that real amount is kept
        // either way. See `IslandReport::shed`'s own doc for why that can
        // make it disagree with the event log.
        for (index, (members, response)) in
            out.islands.members.iter().zip(&out.responses).enumerate()
        {
            let Some(response) = response else { continue };

            // Mirrors `record_step`'s own shed arithmetic exactly — the same
            // `shed_fraction * served_before[index]` — rather than a second,
            // independently-written computation that could drift from it.
            let shed = if response.shed_fraction > 0.0 {
                response.shed_fraction * served_before[index]
            } else {
                0.0
            };

            for &bus in members {
                let history = &mut bus_history[bus];

                let candidate = response.condition;
                history.condition = Some(match history.condition {
                    Some(existing) if severity(existing) >= severity(candidate) => existing,
                    _ => candidate,
                });

                // Absolute Hz: `Response`'s own fields are deviations from
                // nominal, and the wire contract (and this field) wants the
                // frequency itself — 59.7, not -0.3.
                let nadir = nominal + response.nadir_hz;
                history.nadir_hz = Some(match history.nadir_hz {
                    Some(existing) if existing <= nadir => existing,
                    _ => nadir,
                });

                let rocof = response.rocof_hz_s;
                history.rocof_hz_s = Some(match history.rocof_hz_s {
                    Some(existing) if existing.abs() >= rocof.abs() => existing,
                    _ => rocof,
                });

                // Last-write-wins, not a severity maximum — see
                // `IslandReport::settling_hz` for why this one field is
                // deliberately a final-instant quantity rather than an
                // extremum accumulated across the run.
                history.settling_hz = Some(nominal + response.settling_hz);

                history.shed += shed;
            }
        }

        // Checked before "nothing fired": a step can find nothing left to
        // trip or shed and still be the step that reveals every remaining
        // island is dead. That is exactly what a lone, load-free generator
        // island — left over from an earlier split — looks like, and
        // reporting it `Settled` would call a fully de-energized network a
        // steady state.
        if is_collapsed(&out.islands, &case.buses, &out.responses) {
            break CascadeOutcome::Collapsed;
        }

        let no_trips = out.fired.is_empty();
        let no_sheds = out.responses.iter().flatten().all(|r| r.sheds.is_empty());
        if no_trips && no_sheds {
            break CascadeOutcome::Settled;
        }

        steps += 1;
        let at = elapsed + out.at;

        let effects = record_step(&case, &out, at, &served_before, island_count);
        events.extend(effects.events);
        shed_total += effects.shed;
        island_count = effects.island_count;

        elapsed = at;
    };

    // Computed once, here, rather than folded into the loop above: a de-
    // energized island's load and a UFLS record kept step by step are two
    // different kinds of number (a snapshot of the final state, and a
    // running sum of past events), and adding them incrementally in two
    // places is how the two come to disagree. One addition, at the end, is
    // the only version of this number there is.
    let final_islands = Islands::of(&case);
    let mut dead_load = 0.0;
    for members in &final_islands.members {
        if Subcase::extract(&case, members).is_none() {
            dead_load += Balance::of(&case.buses, members).load;
        }
    }
    let load_lost = dead_load + shed_total;

    // Once, here, against the final partition — never per step. Every
    // intermediate island the loop passed through was a transient state on
    // its way to this one, and an AC solve is exactly the expensive
    // verification a DC-model loop exists to defer.
    let mut reports = verify_islands(
        &case,
        &final_islands,
        verification.limits,
        verification.options,
    );

    // `verify_islands` never sees a frequency `Response` — see its own doc
    // comment — so every report comes back with `condition: None` and the
    // three frequency fields `None`/`shed: 0.0`. This is where that gap gets
    // closed, from `bus_history`, the peak severity (or, for `settling_hz`
    // and `shed`, the last-write / running-sum — see their own doc comments)
    // the loop above accumulated.
    //
    // A final island can differ from the partition any one `step` call
    // computed — a call's own fired trips are applied to `case` at the very
    // end of `step`, after `islands`/`responses` were already captured
    // against the topology as it stood at the *start* of that call. So a
    // final island is matched back by its lowest-index member, not by
    // island identity: bus indices never move under re-partitioning, which
    // is what makes `bus_history` keyable by bus at all, and "lowest
    // member stands for the island" is the same convention `representative`
    // uses elsewhere in this module. `None` results exactly when either no
    // step ever ran (an immediately truncated cascade) or every observation
    // of that bus was itself a machine-less island — both honest, distinct
    // answers from `Some(Stable)`, not a placeholder for "nobody checked".
    //
    // One acknowledged gap: a call's own fired trips are not reflected in
    // `islands`/`responses` from that same call — see above — so if that
    // call's firing is what both collapses the network and newly splits it,
    // the newly-split piece's very first moment as its own island is never
    // separately observed. Its buses still carry whatever `bus_history`
    // already held from being part of a larger island a moment earlier,
    // which is the same staleness `is_collapsed` above already tolerates,
    // for the same reason — there is no further call to refine it with.
    for report in &mut reports {
        if let Some(&lowest) = report.members.first() {
            let history = bus_history[lowest];
            report.condition = history.condition;
            report.nadir_hz = history.nadir_hz;
            report.rocof_hz_s = history.rocof_hz_s;
            report.settling_hz = history.settling_hz;
            report.shed = history.shed;
        }
    }

    Ok(Cascade {
        outcome,
        events,
        islands: final_islands,
        load_lost,
        steps,
        elapsed,
        reports,
    })
}

/// Opens every initiating branch simultaneously at `t = 0` and logs each as
/// a `Trip`, returning the log and the island count that opening left
/// behind — the baseline the loop's own island-count tracking starts from.
///
/// # Panics
///
/// Panics if `initiating` names a branch index `case` does not have.
fn open_initiating(case: &mut Case, initiating: &[usize]) -> (Vec<Event>, usize) {
    for &branch in initiating {
        assert!(
            branch < case.n_branch(),
            "initiating outage names branch {branch}, which does not exist in a \
             case with {} branches",
            case.n_branch()
        );
        case.branches[branch].in_service = false;
    }

    // One partition for every initiating branch's event, rather than one
    // computed as each branch opens: they are simultaneous at t = 0, so the
    // islands any one of them belongs to are the islands left once all of
    // them are already open, not a partition only some of them caused.
    let opened = Islands::of(case);
    let mut events = Vec::with_capacity(initiating.len());
    for &branch in initiating {
        let from = case.branches[branch].from;
        events.push(Event {
            at: 0.0,
            kind: EventKind::Trip,
            element: case.branches[branch].id.clone(),
            detail: "initiating outage".to_string(),
            island: Some(opened.of_bus[from]),
        });
    }

    (events, opened.count())
}

/// What one step of the cascade contributed to the log: `Trip` events for
/// what fired, `Shed` events for what UFLS operated, and an `Island` event
/// if this step split the network further than it already was.
struct StepEffects {
    events: Vec<Event>,
    /// Per-unit load this step's UFLS shed, across every island — computed
    /// against `served_before`, since `Response::shed_fraction` is a
    /// fraction of a load `step` had already reduced by the time it comes
    /// back out.
    shed: f64,
    /// The island count after this step's own fired trips.
    island_count: usize,
}

/// Turns one step's [`Outcome`] into log entries, all stamped with the same
/// coalesced `at` — see the module docs for why every event in one step
/// shares a single instant rather than each candidate's own finer time.
fn record_step(
    case: &Case,
    out: &Outcome,
    at: f64,
    served_before: &[f64],
    island_count: usize,
) -> StepEffects {
    let mut events = Vec::new();
    let mut shed = 0.0;

    for candidate in &out.fired {
        let branch = &case.branches[candidate.branch];
        events.push(Event {
            at,
            kind: EventKind::Trip,
            element: branch.id.clone(),
            detail: trip_detail(candidate),
            island: Some(out.islands.of_bus[branch.from]),
        });
    }

    for (index, response) in out.responses.iter().enumerate() {
        let Some(response) = response else { continue };

        if response.shed_fraction > 0.0 {
            shed += response.shed_fraction * served_before[index];
        }

        for stage in &response.sheds {
            events.push(Event {
                at,
                kind: EventKind::Shed,
                element: representative(&out.islands, index, case),
                detail: format!("stage {} · {:.0}%", stage.stage, stage.fraction * 100.0),
                island: Some(index),
            });
        }
    }

    // Recomputed on the case as this step actually left it — after its own
    // fired trips — because that is the only partition that can show this
    // step's own split. `out.islands` is the partition from before those
    // trips, by contract; comparing against it here is exactly what would
    // miss the split this step just caused.
    let after = Islands::of(case).count();
    if after > island_count {
        // Naming which specific island is new would need a mapping from the
        // pre-split partition to the post-split one, which `Islands::of`
        // does not provide, and which a step that splits one island into
        // three would leave ambiguous anyway. The trip that caused the
        // split is unambiguous — it is the earliest fired candidate, in
        // `collect`'s canonical branch-index order — and it is why this
        // event exists at all.
        let trigger = out
            .fired
            .first()
            .map_or_else(|| case.name.clone(), |c| case.branches[c.branch].id.clone());
        events.push(Event {
            at,
            kind: EventKind::Island,
            element: trigger,
            detail: format!("{island_count} islands \u{2192} {after}"),
            island: None,
        });
    }

    StepEffects {
        events,
        shed,
        island_count: after,
    }
}

/// Ranks an [`IslandCondition`] for the sticky severity tracking in
/// [`simulate`]: `Stable < Recovering < Shedding < Collapsed`, matching the
/// order the variants are declared in on `IslandCondition` itself in
/// `bs-frequency`.
///
/// Kept local to this module rather than added as a derived `Ord` on
/// `IslandCondition` itself: severity-as-a-maximum is specific to how a
/// cascade accumulates a condition across many steps, and `bs-frequency`
/// has no such accumulation of its own to justify carrying the ordering.
fn severity(condition: IslandCondition) -> u8 {
    match condition {
        IslandCondition::Stable => 0,
        IslandCondition::Recovering => 1,
        IslandCondition::Shedding => 2,
        IslandCondition::Collapsed => 3,
    }
}

/// Whether every island still carrying load has died.
///
/// An island with no load at all — a generator left standing alone after
/// everything it served separated away from it — is excluded rather than
/// counted as evidence of collapse: its own frequency is trivially stable
/// with nothing to balance, and that stability says nothing about whether
/// the network as a whole still serves anyone. `Collapsed` is a claim about
/// service, not about every island's rotor being at rest.
fn is_collapsed(islands: &Islands, buses: &[Bus], responses: &[Option<Response>]) -> bool {
    let mut any_load = false;

    for (members, response) in islands.members.iter().zip(responses) {
        let load = Balance::of(buses, members).load;
        if load <= 0.0 {
            continue;
        }
        any_load = true;

        let dead = match response {
            None => true,
            Some(r) => r.condition == IslandCondition::Collapsed,
        };
        if !dead {
            return false;
        }
    }

    any_load
}

/// What a trip candidate's cause was, in the operator's shorthand.
fn trip_detail(candidate: &TripCandidate) -> String {
    match candidate.cause {
        TripCause::Overcurrent => format!("{:.0}% · oc", candidate.loading * 100.0),
        TripCause::Distance { zone } => format!("dist zone {zone}"),
    }
}

/// The bus that stands in for an island in an event that cannot point at one
/// specific line — a shed applies pro-rata across every PQ bus in the
/// island, so no single bus caused it. The lowest-index member is picked
/// because `Islands::members` is already sorted that way, which makes the
/// choice deterministic without a separate rule.
fn representative(islands: &Islands, index: usize, case: &Case) -> String {
    islands.members[index]
        .first()
        .map_or_else(|| case.name.clone(), |&bus| case.buses[bus].id.clone())
}

#[cfg(test)]
mod tests {
    use super::{CascadeOutcome, Limits};

    /// The documented defaults: 100 steps, a one-hour horizon. A caller
    /// reading `Limits::default()` in isolation has no other way to know
    /// how much cascade that actually buys, so the numbers here have to
    /// match what the doc comment above claims.
    #[test]
    fn limits_default_matches_the_documented_values() {
        assert_eq!(
            Limits::default(),
            Limits {
                max_steps: 100,
                horizon: 3600.0,
            }
        );
    }

    /// `Truncated` and `Settled` both follow a step, but they are not
    /// interchangeable — the whole reason `Truncated` exists as its own
    /// variant is that an interrupted run must never compare equal to a
    /// resolved one. If this ever failed, `assert_eq!(outcome,
    /// CascadeOutcome::Settled)` in the loop's own tests would stop meaning
    /// what it says.
    #[test]
    fn truncated_and_settled_are_distinct_outcomes() {
        assert_ne!(
            CascadeOutcome::Truncated { steps: 3 },
            CascadeOutcome::Settled
        );
    }
}
