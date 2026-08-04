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
    /// Lower limit on reactive output, per unit.
    ///
    /// Infinite by default, which means unlimited rather than zero. A
    /// generator with no stated limits is one whose limits are unknown, and
    /// silently clamping it to zero would turn missing data into a voltage
    /// collapse that looks like a finding.
    pub q_min: f64,
    /// Upper limit on reactive output, per unit. Infinite by default.
    pub q_max: f64,
    /// Lower limit on real output, per unit. Negative infinity by default.
    ///
    /// Infinity rather than `Option` for the same reason `q_min` uses it:
    /// unbounded is a real and common state, and infinity is what it means.
    pub p_min: f64,
    /// Upper limit on real output, per unit. Infinite by default.
    pub p_max: f64,
    /// Inertia constant, seconds, on the machine's own rating.
    ///
    /// `Option`, unlike the limits above, because there is no numeric value
    /// that means "unknown" here. Zero inertia says the island has no rotating
    /// mass and collapses on any imbalance; infinite inertia says frequency
    /// never moves. Both are confident physical claims, and an unpopulated
    /// import must make neither.
    pub inertia_h: Option<f64>,
    /// Governor droop, per unit, dimensionless. Typically 0.05.
    ///
    /// `Option` for a reason distinct from `inertia_h`'s: a machine that
    /// states no droop has no governor response recorded, which is not the
    /// same claim as a governor with zero droop — zero droop is an
    /// infinitely stiff response, and manufacturing that from silence would
    /// be as dishonest as manufacturing zero inertia from it.
    pub droop_r: Option<f64>,
    /// Shunt conductance to ground at this bus, per unit.
    pub g_shunt: f64,
    /// Shunt susceptance to ground at this bus, per unit. Positive is
    /// capacitive — a capacitor bank supporting voltage; negative is a reactor
    /// absorbing it.
    pub b_shunt: f64,
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
            q_min: f64::NEG_INFINITY,
            q_max: f64::INFINITY,
            p_min: f64::NEG_INFINITY,
            p_max: f64::INFINITY,
            inertia_h: None,
            droop_r: None,
            g_shunt: 0.0,
            b_shunt: 0.0,
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
            q_min: f64::NEG_INFINITY,
            q_max: f64::INFINITY,
            p_min: f64::NEG_INFINITY,
            p_max: f64::INFINITY,
            inertia_h: None,
            droop_r: None,
            g_shunt: 0.0,
            b_shunt: 0.0,
        }
    }

    /// Sets the shunt admittance to ground at this bus.
    ///
    /// Capacitor banks and reactors are modelled here rather than as branches
    /// to a ground bus, because they are a property of the bus and adding a
    /// fictitious bus per shunt would corrupt the index space every result is
    /// reported in.
    #[must_use]
    pub fn with_shunt(mut self, g_shunt: f64, b_shunt: f64) -> Self {
        self.g_shunt = g_shunt;
        self.b_shunt = b_shunt;
        self
    }

    /// Sets the reactive output limits.
    ///
    /// A PV bus holds its scheduled voltage by producing whatever reactive
    /// power that takes. Real generators cannot, and a solution that has one
    /// producing 40 per unit to hold 1.02 is not a solution — it is a warning
    /// that the network cannot support that voltage.
    #[must_use]
    pub fn with_reactive_limits(mut self, q_min: f64, q_max: f64) -> Self {
        self.q_min = q_min;
        self.q_max = q_max;
        self
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
            q_min: f64::NEG_INFINITY,
            q_max: f64::INFINITY,
            p_min: f64::NEG_INFINITY,
            p_max: f64::INFINITY,
            inertia_h: None,
            droop_r: None,
            g_shunt: 0.0,
            b_shunt: 0.0,
        }
    }

    /// Sets the real output limits, which bound re-dispatch.
    #[must_use]
    pub fn with_real_limits(mut self, p_min: f64, p_max: f64) -> Self {
        self.p_min = p_min;
        self.p_max = p_max;
        self
    }

    /// Sets the machine data an island frequency model needs.
    #[must_use]
    pub fn with_machine(mut self, inertia_h: f64, droop_r: f64) -> Self {
        self.inertia_h = Some(inertia_h);
        self.droop_r = Some(droop_r);
        self
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
    /// Off-nominal turns ratio, applied at the from-end. 1.0 for a line.
    ///
    /// A transformer is a branch with a ratio, not a separate entity. Modelling
    /// it separately would double every loop that walks the network for no
    /// gain: the equations differ by this one factor.
    pub tap: f64,
    /// Phase shift in radians, applied at the from-end. Zero for a line and
    /// for an ordinary transformer.
    pub shift: f64,
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
            tap: 1.0,
            shift: 0.0,
        }
    }

    /// Sets the off-nominal turns ratio and phase shift.
    ///
    /// The ratio is at the from-end, following the convention MATPOWER and
    /// pandapower both use, so an imported case needs no reinterpretation.
    #[must_use]
    pub fn with_transformer(mut self, tap: f64, shift: f64) -> Self {
        self.tap = tap;
        self.shift = shift;
        self
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
