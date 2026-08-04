//! The cascade loop: repeated stepping from an initiating outage to a
//! settled, truncated, or collapsed end state.
//!
//! [`step`] answers what happens in one instant. A cascade is a sequence of
//! those instants, so this module owns exactly the things a single step
//! cannot: opening the branches that started it, deciding when to stop, and
//! turning the accumulated changes into the time-ordered log an operator
//! reads back. This half of the module is the log's vocabulary — the event
//! and outcome types the loop fills in.
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

use crate::island::Islands;

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
    /// Which island this event belongs to, where that is meaningful. `None`
    /// for an event that is not about one particular island — an initiating
    /// outage before islanding is known to have produced more than one
    /// piece yet, or a split that by definition touches more than one.
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
    /// Computed once, from the final case state and a running tally kept
    /// during the loop — not incrementally recomputed at each step and
    /// folded into a second running total, which is how the two versions
    /// drift apart.
    pub load_lost: f64,
    /// Steps that produced a trip or a shed. A step that found nothing to
    /// do does not count — it is the evidence the cascade is over, not a
    /// step of it.
    pub steps: usize,
    /// Simulated seconds elapsed.
    pub elapsed: f64,
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
