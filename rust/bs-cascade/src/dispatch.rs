//! Real-power dispatch within an island: closing what headroom allows.
//!
//! A cascade splits the network into islands, and each island's generation
//! no longer matches its load — [`Balance`] measures that mismatch and
//! [`rebalance`] decides how the island's own units respond to it. Dispatch
//! never sheds load: it closes what the fleet's headroom allows and reports
//! the rest as [`Plan::residual`], which becomes the frequency model's
//! imbalance. Shedding belongs there, because that is what gives a shed a
//! frequency to be caused by — a shed with no frequency behind it would make
//! the island's nadir decorative.
//!
//! `gap` is positive for a shortfall (load exceeds generation), matching
//! `bs-frequency`'s imbalance sign. The two crates disagreeing here would
//! make an island shed load when it has surplus generation, and the result
//! would look plausible rather than obviously wrong.

use bs_case::{Bus, Case};

/// One generator's dispatchable output and its real-power limits.
///
/// `bus` travels with the electrical quantities so a [`Plan`] can name where
/// each change goes without the caller re-deriving it from position in a
/// list — list order is not bus order, and [`rebalance`] must not depend on
/// it either.
#[derive(Debug, Clone, Copy, PartialEq)]
pub struct Dispatchable {
    /// Bus index this unit is attached to.
    pub bus: usize,
    /// Current real-power output.
    pub output: f64,
    /// Lower real-power limit. `f64::NEG_INFINITY` means unlimited.
    pub p_min: f64,
    /// Upper real-power limit. `f64::INFINITY` means unlimited.
    pub p_max: f64,
}

impl Dispatchable {
    /// Room to raise output before hitting `p_max`.
    #[must_use]
    pub fn headroom_up(&self) -> f64 {
        self.p_max - self.output
    }

    /// Room to lower output before hitting `p_min`.
    #[must_use]
    pub fn headroom_down(&self) -> f64 {
        self.output - self.p_min
    }
}

/// One island's real-power balance.
#[derive(Debug, Clone, Copy, PartialEq)]
pub struct Balance {
    /// Sum of non-negative `p_injection` among the given members.
    pub generation: f64,
    /// Sum of `-p_injection` among members with negative `p_injection` — a
    /// magnitude, not a signed load.
    pub load: f64,
    /// `load - generation`. Positive for a shortfall, negative for a
    /// surplus, matching `bs-frequency`'s imbalance convention.
    pub gap: f64,
}

impl Balance {
    /// Sums generation and load over one island's members.
    ///
    /// Walks `members` in the order given rather than sorting first, so the
    /// result is deterministic for whatever order the caller already
    /// establishes (typically `Islands::members`, already sorted by bus)
    /// without this function silently imposing a second one.
    #[must_use]
    pub fn of(buses: &[Bus], members: &[usize]) -> Self {
        let mut generation = 0.0;
        let mut load = 0.0;

        for &bus in members {
            let p = buses[bus].p_injection;
            if p >= 0.0 {
                generation += p;
            } else {
                load -= p;
            }
        }

        Self {
            generation,
            load,
            gap: load - generation,
        }
    }
}

/// A dispatch decision: per-bus output changes and what they could not cover.
#[derive(Debug, Clone, PartialEq)]
pub struct Plan {
    /// `(bus, delta)` pairs, ordered by bus index, to add to `p_injection`.
    pub changes: Vec<(usize, f64)>,
    /// What the fleet's headroom could not close. Positive for a remaining
    /// shortfall, negative for a remaining surplus, zero when the gap was
    /// fully covered.
    pub residual: f64,
}

/// Chooses how an island closes a real-power gap within available headroom.
///
/// `gap` is positive for a shortfall. Returns the per-bus output changes and
/// whatever the fleet could not cover, which becomes the frequency model's
/// imbalance — dispatch never sheds load, because a shed with no frequency
/// behind it makes the island's nadir decorative.
///
/// The policy is pro-rata on headroom: unit `i` takes
/// `gap × headroom(i) / Σ headroom`, clamped so no unit exceeds its own
/// headroom. This was a human decision, not a derivation — merit order
/// (concentrate the response on the biggest units, for deeper and more
/// localised cascades) and a participation factor on current output (simple,
/// but it hands the largest share to the unit already closest to its
/// ceiling) were both live alternatives. Pro-rata was chosen because it
/// spreads the gap across the whole fleet so every unit reaches its limit at
/// the same moment rather than one being pinned while another sits idle,
/// which tends to keep islands alive and makes cascades come out shallower
/// than merit order would. That is a property of the *policy*, not of the
/// network, so it belongs here where it can be seen rather than emerging as
/// an unexplained artifact of every downstream result.
///
/// `p_min`/`p_max` default to `f64::NEG_INFINITY`/`f64::INFINITY`, meaning
/// genuinely unlimited rather than a large number. A pro-rata share of
/// `inf / inf` is `NaN`, and injecting `NaN` into `p_injection` would poison
/// the whole solve silently. So: if any unit has infinite headroom in the
/// needed direction, the gap is fully coverable, and it is split equally, in
/// bus order, among the infinite-headroom units only — every finite unit is
/// left untouched rather than mixed into a ratio that can't be formed.
///
/// Changes come back ordered by bus index, so applying them is a canonical
/// sequence of floating-point additions rather than one that depends on
/// unit order.
#[must_use]
pub fn rebalance(gap: f64, units: &[Dispatchable]) -> Plan {
    if gap == 0.0 {
        return Plan {
            changes: Vec::new(),
            residual: 0.0,
        };
    }

    let need = gap.abs();
    let headroom = |unit: &Dispatchable| -> f64 {
        if gap > 0.0 {
            unit.headroom_up()
        } else {
            unit.headroom_down()
        }
    };

    let infinite: Vec<&Dispatchable> = units
        .iter()
        .filter(|unit| headroom(unit).is_infinite())
        .collect();

    let mut changes: Vec<(usize, f64)> = if infinite.is_empty() {
        let total: f64 = units.iter().map(headroom).sum();
        // `need / total` is at most 1.0 whenever total >= need, which is
        // exactly the case where every unit's share stays within its own
        // headroom without a separate clamp. Below need, the min(1.0) makes
        // every unit contribute its full headroom instead, and the shortfall
        // becomes residual.
        let factor = if total > 0.0 {
            (need / total).min(1.0)
        } else {
            0.0
        };

        units
            .iter()
            .map(|unit| (unit.bus, headroom(unit) * factor))
            .filter(|&(_, share)| share > 0.0)
            .collect()
    } else {
        // Equal split among the infinite-headroom units, counted by folding
        // rather than casting `len()` to `f64` — the workspace denies
        // float-to-integer casts, and this sidesteps the opposite direction
        // too.
        let count = infinite.iter().fold(0.0_f64, |acc, _| acc + 1.0);
        let share = need / count;
        infinite.iter().map(|unit| (unit.bus, share)).collect()
    };

    if gap < 0.0 {
        for (_, delta) in &mut changes {
            *delta = -*delta;
        }
    }

    changes.sort_by_key(|&(bus, _)| bus);

    let covered: f64 = changes.iter().map(|&(_, delta)| delta.abs()).sum();
    let residual_magnitude = (need - covered).max(0.0);
    let residual = if gap > 0.0 {
        residual_magnitude
    } else {
        -residual_magnitude
    };

    Plan { changes, residual }
}

/// Applies a plan's changes to a case's `p_injection`, in bus order.
///
/// # Panics
///
/// Panics if any `bus` in `plan.changes` is out of range for `case.buses`.
pub fn apply(case: &mut Case, plan: &Plan) {
    for &(bus, delta) in &plan.changes {
        case.buses[bus].p_injection += delta;
    }
}
