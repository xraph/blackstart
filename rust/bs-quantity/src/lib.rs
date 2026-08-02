//! Per-unit and SI quantities for the solver.
//!
//! This mirrors the Go `pkg/quantity` package, and the mirroring is the point:
//! a value crosses the gRPC boundary in SI, gets normalized against a base
//! here, and must mean the same thing on both sides. Mixing per-unit and
//! physical units is the classic power-systems bug class, and it produces a
//! plausible wrong answer rather than a crash.
//!
//! The solver works almost entirely in per-unit — that is what per-unit is
//! for — so these types are built for that direction, with conversion pushed
//! to the boundaries rather than sprinkled through an iteration.

#![forbid(unsafe_code)]

use std::fmt;

/// The three-phase line-to-line factor. Rust's `std::f64::consts` offers
/// `SQRT_2` but not this one.
pub const SQRT_3: f64 = 1.732_050_807_568_877_2;

/// A per-unit reference base.
///
/// The conventions are the standard three-phase ones, written out because
/// getting them wrong is a silent factor-of-root-three error:
///
/// - `s_base` is three-phase apparent power, in volt-amperes.
/// - `v_base` is line-to-line voltage, in volts.
/// - `z_base = v_base² / s_base`
/// - `i_base = s_base / (√3 · v_base)`
#[derive(Debug, Clone, Copy, PartialEq)]
pub struct Base {
    s_base: f64,
    v_base: f64,
}

/// Why a base could not be constructed.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum BaseError {
    /// Apparent power base was zero, negative, or not finite.
    ApparentPower,
    /// Voltage base was zero, negative, or not finite.
    Voltage,
}

impl fmt::Display for BaseError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::ApparentPower => write!(f, "apparent power base must be positive and finite"),
            Self::Voltage => write!(f, "voltage base must be positive and finite"),
        }
    }
}

impl std::error::Error for BaseError {}

impl Base {
    /// Builds a base from the units engineers quote: three-phase MVA and
    /// line-to-line kV.
    ///
    /// A degenerate base is rejected rather than tolerated. It is not an edge
    /// case to degrade around — it means the network model is wrong, and every
    /// value normalized against it would be meaningless.
    ///
    /// # Errors
    ///
    /// Returns [`BaseError`] when either argument is not positive and finite.
    pub fn new(s_base_mva: f64, v_base_kv: f64) -> Result<Self, BaseError> {
        if !positive_finite(s_base_mva) {
            return Err(BaseError::ApparentPower);
        }
        if !positive_finite(v_base_kv) {
            return Err(BaseError::Voltage);
        }
        Ok(Self {
            s_base: s_base_mva * 1e6,
            v_base: v_base_kv * 1e3,
        })
    }

    /// Three-phase apparent power base, in volt-amperes.
    #[must_use]
    pub const fn s_base(&self) -> f64 {
        self.s_base
    }

    /// Line-to-line voltage base, in volts.
    #[must_use]
    pub const fn v_base(&self) -> f64 {
        self.v_base
    }

    /// Impedance base, in ohms.
    #[must_use]
    pub fn z_base(&self) -> f64 {
        self.v_base * self.v_base / self.s_base
    }

    /// Admittance base, in siemens.
    #[must_use]
    pub fn y_base(&self) -> f64 {
        self.s_base / (self.v_base * self.v_base)
    }

    /// Line current base, in amperes.
    ///
    /// The √3 is the three-phase line-to-line convention. Omitting it gives a
    /// current 73% too high — large enough to look like a modelling mistake
    /// rather than a units mistake, and so to be hunted in the wrong place.
    #[must_use]
    pub fn i_base(&self) -> f64 {
        self.s_base / (SQRT_3 * self.v_base)
    }

    /// Converts megawatts to per-unit active power.
    #[must_use]
    pub fn mw_to_pu(&self, mw: f64) -> f64 {
        mw * 1e6 / self.s_base
    }

    /// Converts per-unit active power to megawatts.
    #[must_use]
    pub fn pu_to_mw(&self, pu: f64) -> f64 {
        pu * self.s_base / 1e6
    }

    /// Converts ohms to per-unit impedance.
    #[must_use]
    pub fn ohm_to_pu(&self, ohm: f64) -> f64 {
        ohm / self.z_base()
    }

    /// Converts per-unit impedance to ohms.
    #[must_use]
    pub fn pu_to_ohm(&self, pu: f64) -> f64 {
        pu * self.z_base()
    }

    /// Converts kilovolts to per-unit voltage.
    #[must_use]
    pub fn kv_to_pu(&self, kv: f64) -> f64 {
        kv * 1e3 / self.v_base
    }

    /// Converts per-unit voltage to kilovolts.
    #[must_use]
    pub fn pu_to_kv(&self, pu: f64) -> f64 {
        pu * self.v_base / 1e3
    }

    /// Rebases a per-unit impedance from another base onto this one.
    ///
    /// Equipment impedances are published on the equipment's own rating, not
    /// the system base — a transformer's 8% reactance is 8% on its own MVA and
    /// its own kV. Folding that onto the system base is routine, and a routine
    /// source of error, so it lives here rather than at every call site.
    #[must_use]
    pub fn rebase_impedance(&self, pu: f64, from: &Self) -> f64 {
        pu * (from.z_base() / self.z_base())
    }
}

impl fmt::Display for Base {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "{:.4} MVA / {:.4} kV",
            self.s_base / 1e6,
            self.v_base / 1e3
        )
    }
}

fn positive_finite(v: f64) -> bool {
    v > 0.0 && v.is_finite()
}

#[cfg(test)]
mod tests {
    use super::*;

    /// Relative comparison. Exact float equality is banned by the lint set for
    /// exactly the reason it would be wrong here.
    fn close(got: f64, want: f64, tol: f64) -> bool {
        (got - want).abs() <= tol * want.abs().max(1.0)
    }

    /// Worked example from any power-systems text: 100 MVA, 138 kV.
    #[test]
    fn derived_bases_match_the_textbook() {
        let b = Base::new(100.0, 138.0).unwrap();

        assert!(close(b.s_base(), 100e6, 1e-12));
        assert!(close(b.v_base(), 138e3, 1e-12));
        assert!(close(b.z_base(), 190.44, 1e-4), "z_base = {}", b.z_base());
        assert!(close(b.i_base(), 418.3697, 1e-4), "i_base = {}", b.i_base());
    }

    #[test]
    fn impedance_and_admittance_bases_are_reciprocal() {
        for (mva, kv) in [(100.0, 138.0), (100.0, 345.0), (1.0, 0.48), (2500.0, 765.0)] {
            let b = Base::new(mva, kv).unwrap();
            assert!(close(b.z_base() * b.y_base(), 1.0, 1e-12));
        }
    }

    /// Omitting the √3 gives a current 73% too high, which reads as a
    /// modelling error rather than a units error.
    #[test]
    fn current_base_uses_the_three_phase_convention() {
        let b = Base::new(100.0, 138.0).unwrap();
        let single_phase_wrong = b.s_base() / b.v_base();

        assert!(!close(b.i_base(), single_phase_wrong, 1e-6));
        assert!(close(single_phase_wrong / b.i_base(), SQRT_3, 1e-12));
    }

    #[test]
    fn degenerate_bases_are_rejected() {
        assert_eq!(Base::new(0.0, 138.0), Err(BaseError::ApparentPower));
        assert_eq!(Base::new(-100.0, 138.0), Err(BaseError::ApparentPower));
        assert_eq!(
            Base::new(f64::INFINITY, 138.0),
            Err(BaseError::ApparentPower)
        );
        assert_eq!(Base::new(f64::NAN, 138.0), Err(BaseError::ApparentPower));
        assert_eq!(Base::new(100.0, 0.0), Err(BaseError::Voltage));
        assert_eq!(Base::new(100.0, -138.0), Err(BaseError::Voltage));
    }

    #[test]
    fn conversions_round_trip() {
        let b = Base::new(100.0, 138.0).unwrap();

        for mw in [0.0, 1.0, -45.5, 1234.5] {
            assert!(close(b.pu_to_mw(b.mw_to_pu(mw)), mw, 1e-12));
        }
        for ohm in [1.0, 190.44, 0.001] {
            assert!(close(b.pu_to_ohm(b.ohm_to_pu(ohm)), ohm, 1e-12));
        }
        for kv in [138.0, 1.0, 765.0] {
            assert!(close(b.pu_to_kv(b.kv_to_pu(kv)), kv, 1e-12));
        }
    }

    /// The conventional system base is 100 MVA, so per-unit power on it is a
    /// number an engineer reads directly.
    #[test]
    fn hundred_mva_base_makes_per_unit_readable() {
        let b = Base::new(100.0, 138.0).unwrap();
        assert!(close(b.mw_to_pu(50.0), 0.5, 1e-12));
        assert!(close(b.mw_to_pu(100.0), 1.0, 1e-12));
    }

    /// A transformer's 8% reactance is 8% on its own rating, not on the system
    /// base.
    #[test]
    fn rebasing_an_impedance_scales_by_the_base_ratio() {
        let system = Base::new(100.0, 138.0).unwrap();
        let equipment = Base::new(50.0, 138.0).unwrap();

        // Halving the MVA base doubles z_base, so the same physical impedance
        // is twice as many per-unit on the smaller equipment base.
        let on_system = system.rebase_impedance(0.08, &equipment);
        assert!(close(on_system, 0.16, 1e-12), "got {on_system}");

        // The physical value is unchanged, which is the property that matters.
        assert!(close(
            system.pu_to_ohm(on_system),
            equipment.pu_to_ohm(0.08),
            1e-12
        ));
    }
}
