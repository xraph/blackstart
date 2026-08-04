//! Aggregating an island's machines into one equivalent unit.
//!
//! A cascade needs an island's frequency, not each machine's rotor angle. The
//! standard reduction is one equivalent machine with generation-weighted
//! inertia and summed droop response, which is what this builds.

use bs_case::{Bus, BusKind};

/// One island's machines, reduced to a single equivalent unit.
#[derive(Debug, Clone, Copy, PartialEq)]
pub struct Aggregate {
    /// Generation-weighted inertia constant, seconds.
    pub inertia_h: f64,
    /// Summed droop response `Σ(1/Rᵢ)(Sᵢ/S_base)`, per unit on the island's
    /// generation base. Zero when no machine has a governor.
    pub inverse_droop: f64,
    /// Total machine rating in the island, per unit. The base the two figures
    /// above are expressed on.
    pub generation_base: f64,
    /// How many machines contributed. Carried so a result can say whether a
    /// frequency came from a fleet or from one unit.
    pub machines: usize,
}

impl Aggregate {
    /// Reduces the named buses to one equivalent machine.
    ///
    /// `None` when no member is a machine carrying inertia data. That is not
    /// the same as an island with zero inertia — zero inertia says the island
    /// collapses on any imbalance, and asserting that about a network nobody
    /// supplied data for would turn a gap in an import into a finding.
    ///
    /// The machine rating `Sᵢ` is taken from `p_max`, because the case carries
    /// no separate machine rating. Where `p_max` is infinite — meaning
    /// unlimited rather than large — present output stands in. Both are
    /// approximations, and the second is the weaker one.
    ///
    /// `default_droop_r` is [`Defaults::droop_r`] — a machine that states no
    /// `droop_r` of its own gets this value, not zero governor response.
    /// There is no way, on `Bus`, to say "this machine deliberately has no
    /// governor" — `droop_r` is `Option<f64>` purely because the case format
    /// may simply not have carried the figure, the same reason `inertia_h`
    /// is optional — so treating an absent value as "no response" would
    /// silently turn a data gap into a physical claim (the machine biases
    /// every island it is in toward over-predicting collapse, since the
    /// system's governors do less work than they really would). A droop that
    /// IS present but non-positive is a different case — nonsensical input,
    /// not a gap — and is still excluded below, unchanged.
    #[must_use]
    pub fn of(buses: &[Bus], members: &[usize], default_droop_r: f64) -> Option<Self> {
        let mut weighted_inertia = 0.0;
        let mut inverse_droop = 0.0;
        let mut generation_base = 0.0;
        let mut machines = 0;

        // Members are walked in the order given, which the caller derives from
        // bus array order, so the summation order is canonical.
        for &bus_index in members {
            let bus = &buses[bus_index];

            if !matches!(bus.kind, BusKind::Pv | BusKind::Slack) {
                continue;
            }

            let Some(inertia) = bus.inertia_h else {
                continue;
            };

            let rating = if bus.p_max.is_finite() && bus.p_max > 0.0 {
                bus.p_max
            } else {
                bus.p_injection.abs()
            };

            if rating <= 0.0 {
                continue;
            }

            weighted_inertia += inertia * rating;
            generation_base += rating;
            machines += 1;

            // A missing droop gets `default_droop_r` — see this function's
            // own doc for why that default, rather than zero response, is
            // the honest reading of "the case didn't say". A present but
            // non-positive droop is left alone and excluded: that is a
            // caller stating something nonsensical, not a gap this function
            // has standing to fill in.
            let droop = bus.droop_r.unwrap_or(default_droop_r);
            if droop > 0.0 {
                inverse_droop += rating / droop;
            }
        }

        if machines == 0 || generation_base <= 0.0 {
            return None;
        }

        Some(Self {
            inertia_h: weighted_inertia / generation_base,
            inverse_droop: inverse_droop / generation_base,
            generation_base,
            machines,
        })
    }
}

/// System-wide characteristics used where the model is silent.
///
/// These are turbine and load characteristics, not utility policy, which is
/// why they may have defaults at all. A shedding schedule may not — see
/// `ufls`.
#[derive(Debug, Clone, Copy, PartialEq)]
pub struct Defaults {
    /// Load damping, per unit power per per-unit frequency.
    pub damping: f64,
    /// Fraction of turbine power responding without reheat lag.
    pub high_pressure_fraction: f64,
    /// Reheat time constant, seconds.
    pub reheat_time: f64,
    /// Droop for a machine that states none.
    pub droop_r: f64,
    /// Nominal system frequency, hertz.
    pub nominal_hz: f64,
}

impl Default for Defaults {
    fn default() -> Self {
        Self {
            damping: 1.0,
            high_pressure_fraction: 0.3,
            reheat_time: 8.0,
            droop_r: 0.05,
            nominal_hz: 60.0,
        }
    }
}
