//! Distance protection, as a set of impedance zones.
//!
//! Apparent impedance at the relay is `V²/S`, which in the DC model at
//! `V = 1.0` per unit is `1/|P|`. A zone sees the branch when that impedance
//! falls inside its reach, so a zone with reach `Z` trips at `|P| > 1/Z`.
//!
//! What this approximation represents, and what it does not. DC carries no
//! angle information, so the model cannot tell a fault from a heavy load
//! swing. That is normally a defect; here it is the point, because zone-3
//! relays tripping healthy lines on load is the 2003 Northeast mechanism and a
//! model that could tell the difference would not reproduce it. What is also
//! missing is load-encroachment blinding, which real modern relays do have —
//! so zone 3 here is more willing to trip than a well-set relay, and the bias
//! is toward over-predicting cascades. Documented rather than tuned away,
//! because correcting it needs relay settings nobody has for a synthetic grid.

/// One distance zone.
#[derive(Debug, Clone, Copy, PartialEq)]
pub struct Zone {
    /// Reach as a multiple of the protected branch's own reactance.
    pub reach: f64,
    /// Intentional delay, seconds, on top of the breaker time.
    pub delay: f64,
}

/// A distance relay guarding one branch.
#[derive(Debug, Clone, PartialEq)]
pub struct Distance {
    /// Zones, conventionally narrowest first, though the order does not
    /// matter: the fastest zone that sees the branch is the one that operates.
    pub zones: Vec<Zone>,
    /// Breaker operating time, seconds.
    pub breaker_time: f64,
}

impl Default for Distance {
    fn default() -> Self {
        Self {
            zones: vec![
                Zone {
                    reach: 0.8,
                    delay: 0.0,
                },
                Zone {
                    reach: 1.2,
                    delay: 0.3,
                },
                Zone {
                    reach: 1.5,
                    delay: 1.0,
                },
            ],
            breaker_time: 0.05,
        }
    }
}

impl Distance {
    /// When this relay trips and which zone saw it, or `None`.
    ///
    /// The zone is 1-based, as relay engineers number them, and is carried so
    /// an event log can say *why* rather than only *that*.
    #[must_use]
    pub fn time_to_trip(&self, flow: f64, reactance: f64) -> Option<(f64, usize)> {
        let magnitude = flow.abs();

        // No flow is infinite apparent impedance; no reactance is a branch
        // Case::validate already rejects, but this crate is callable without
        // a validated case and must not divide by it.
        if magnitude <= 0.0 || reactance.abs() <= 0.0 {
            return None;
        }

        let apparent = 1.0 / magnitude;

        let mut best: Option<(f64, usize)> = None;

        for (index, zone) in self.zones.iter().enumerate() {
            let reach = zone.reach * reactance.abs();

            if apparent >= reach {
                continue;
            }

            let at = self.breaker_time + zone.delay;

            // The fastest zone that sees it operates. Taking the first
            // matching zone in list order instead would make every zone-3
            // trip instantaneous.
            let better = match best {
                None => true,
                Some((current, _)) => at < current,
            };

            if better {
                best = Some((at, index + 1));
            }
        }

        best
    }
}
