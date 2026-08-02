//! Buses and branches: the elements a case is made of.

/// How a bus participates in the power flow.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum BusKind {
    /// Load bus: real and reactive injection known, voltage unknown.
    Pq,
    /// Generator bus: real injection and voltage magnitude known.
    Pv,
    /// Reference bus. Its angle is fixed and its injection absorbs the
    /// mismatch, which is why exactly one is required — the equations are
    /// otherwise singular, having no absolute angle reference.
    Slack,
}

/// A bus in the solvable case.
#[derive(Debug, Clone, PartialEq)]
pub struct Bus {
    /// Stable identifier from the platform, carried through so a result can
    /// name the bus it belongs to without a lookup table.
    pub id: String,
    /// How this bus participates.
    pub kind: BusKind,
    /// Net real power injection, per unit. Generation positive, load negative.
    pub p_injection: f64,
    /// Net reactive power injection, per unit.
    pub q_injection: f64,
    /// Voltage magnitude, per unit. Fixed for PV and slack buses; a starting
    /// value for PQ buses.
    pub v_magnitude: f64,
    /// Voltage angle in radians. Fixed for the slack bus; a starting value
    /// elsewhere.
    pub v_angle: f64,
}

impl Bus {
    /// Builds a PQ bus with a flat voltage start.
    #[must_use]
    pub fn load(id: impl Into<String>, p_injection: f64, q_injection: f64) -> Self {
        Self {
            id: id.into(),
            kind: BusKind::Pq,
            p_injection,
            q_injection,
            v_magnitude: 1.0,
            v_angle: 0.0,
        }
    }

    /// Builds a PV bus holding a voltage magnitude.
    #[must_use]
    pub fn generator(id: impl Into<String>, p_injection: f64, v_magnitude: f64) -> Self {
        Self {
            id: id.into(),
            kind: BusKind::Pv,
            p_injection,
            q_injection: 0.0,
            v_magnitude,
            v_angle: 0.0,
        }
    }

    /// Builds the reference bus.
    #[must_use]
    pub fn slack(id: impl Into<String>, v_magnitude: f64) -> Self {
        Self {
            id: id.into(),
            kind: BusKind::Slack,
            p_injection: 0.0,
            q_injection: 0.0,
            v_magnitude,
            v_angle: 0.0,
        }
    }
}

/// A branch: a line, cable, or transformer, in per-unit.
#[derive(Debug, Clone, PartialEq)]
pub struct Branch {
    /// Stable identifier from the platform.
    pub id: String,
    /// Index of the from-bus.
    pub from: usize,
    /// Index of the to-bus.
    pub to: usize,
    /// Series resistance, per unit.
    pub r: f64,
    /// Series reactance, per unit. Must be non-zero: a zero-impedance branch
    /// is a bus merge that topology processing should already have collapsed.
    pub x: f64,
    /// Total line charging susceptance, per unit.
    pub b: f64,
    /// Long-term rating, per unit. Zero means unrated.
    pub rating: f64,
    /// Whether the branch is in service. An out-of-service branch stays in the
    /// case so that contingency analysis can restore it without rebuilding
    /// every index.
    pub in_service: bool,
}

impl Branch {
    /// Builds an in-service branch.
    #[must_use]
    pub fn new(id: impl Into<String>, from: usize, to: usize, r: f64, x: f64) -> Self {
        Self {
            id: id.into(),
            from,
            to,
            r,
            x,
            b: 0.0,
            rating: 0.0,
            in_service: true,
        }
    }

    /// Sets the long-term rating.
    #[must_use]
    pub fn with_rating(mut self, rating: f64) -> Self {
        self.rating = rating;
        self
    }

    /// Sets the total charging susceptance.
    #[must_use]
    pub fn with_charging(mut self, b: f64) -> Self {
        self.b = b;
        self
    }
}
