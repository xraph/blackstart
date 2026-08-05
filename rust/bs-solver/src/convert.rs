//! Converting between the SI wire format and the per-unit solver.
//!
//! This module is where per-unit conversion happens, and it is the only place
//! in the crate that does it. The contract says every wire value is SI; the
//! numerics work entirely in per unit. Scattering the division by a base
//! across the service would mean every new field is a chance to forget one,
//! and a forgotten conversion does not fail — it produces a plausible answer
//! on the wrong scale.

use bs_case::{Branch, Bus, BusKind, Case};
use bs_frequency::{Scheme, Stage};
use bs_protection::{Curve, Distance, Overcurrent, Protection, Settings, Zone};
use bs_quantity::Base;

use crate::proto;

/// Why a wire model could not be converted.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ConvertError {
    /// The per-unit base is not usable.
    Base(String),
    /// A bus kind was unspecified or unrecognised.
    ///
    /// Unspecified is rejected rather than defaulted. Proto3 gives every enum
    /// a zero value whether the sender meant it or not, so silently reading it
    /// as PQ would turn a forgotten field into a network that solves and is
    /// wrong.
    BusKind { bus: usize },
}

impl core::fmt::Display for ConvertError {
    fn fmt(&self, f: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        match self {
            Self::Base(why) => write!(f, "per-unit base: {why}"),
            Self::BusKind { bus } => write!(
                f,
                "bus {bus} has no kind; proto3 defaults an unset enum to zero, \
                 so this is read as a missing field rather than as PQ"
            ),
        }
    }
}

impl std::error::Error for ConvertError {}

/// Converts a wire model into a solvable case, dividing SI by the base.
///
/// # Errors
///
/// Returns [`ConvertError`] when the base is unusable or a bus kind is unset.
pub fn to_case(model: &proto::NetworkModel) -> Result<Case, ConvertError> {
    // The contract carries the base in volt-amperes and volts; Base is stated
    // in MVA and kV, which is what every power-systems text and every input
    // file uses. The factor of a million lives here and nowhere else.
    let base = Base::new(model.s_base_va / 1e6, model.v_base_v / 1e3)
        .map_err(|e| ConvertError::Base(e.to_string()))?;

    let s_base = model.s_base_va;

    // The model's base, and deliberately not `bus.v_nominal_v`, which the
    // contract also carries per bus now. Per unit needs one base per zone, and
    // a transformer puts each end of a branch in a different zone — so reading
    // a bus's own nominal voltage here would not be a more accurate conversion
    // but a different and unfinished per-unit system, with a branch whose two
    // ends disagree about what 1.0 means. `v_nominal_v` is informational: it
    // exists for consumers converting a current into a power, where the bus's
    // own voltage is exactly what is wanted. Nothing in the numerics reads it.
    let v_base = model.v_base_v;

    // Impedance base, ohms. Z_base = V_base² / S_base.
    let z_base = v_base * v_base / s_base;

    let mut buses = Vec::with_capacity(model.buses.len());

    for (index, wire) in model.buses.iter().enumerate() {
        let kind = match proto::BusKind::try_from(wire.kind) {
            Ok(proto::BusKind::Pq) => BusKind::Pq,
            Ok(proto::BusKind::Pv) => BusKind::Pv,
            Ok(proto::BusKind::Slack) => BusKind::Slack,
            _ => return Err(ConvertError::BusKind { bus: index }),
        };

        buses.push(Bus {
            id: wire.id.clone(),
            kind,
            p_injection: wire.p_injection_w / s_base,
            q_injection: wire.q_injection_var / s_base,
            v_magnitude: wire.v_magnitude_v / v_base,
            v_angle: wire.v_angle_rad,
            // Absent means unlimited, not zero. See the contract: a generator
            // with no stated limits has unknown limits.
            q_min: wire.q_min_var.map_or(f64::NEG_INFINITY, |q| q / s_base),
            q_max: wire.q_max_var.map_or(f64::INFINITY, |q| q / s_base),
            // Absent means unlimited, exactly like the reactive limits above.
            p_min: wire.p_min_w.map_or(f64::NEG_INFINITY, |p| p / s_base),
            p_max: wire.p_max_w.map_or(f64::INFINITY, |p| p / s_base),
            // SECONDS and DIMENSIONLESS respectively, never divided by a
            // base. Dividing a dimensionless quantity by `s_base` would be
            // the same class of error, in the opposite direction, as
            // forgetting to divide a power — see the module docs.
            inertia_h: wire.inertia_h_s,
            droop_r: wire.droop_r,
            // Admittance base is the reciprocal of the impedance base.
            g_shunt: wire.g_shunt_s * z_base,
            b_shunt: wire.b_shunt_s * z_base,
        });
    }

    let branches = model
        .branches
        .iter()
        .map(|wire| Branch {
            id: wire.id.clone(),
            from: wire.from_bus as usize,
            to: wire.to_bus as usize,
            r: wire.r_ohm / z_base,
            x: wire.x_ohm / z_base,
            b: wire.b_shunt_s * z_base,
            rating: wire.rating_va / s_base,
            in_service: wire.in_service,
            tap: wire.tap,
            shift: wire.shift_rad,
        })
        .collect();

    Ok(Case::new(model.name.clone(), base, buses, branches))
}

/// Multiplies a per-unit power back into watts.
#[must_use]
pub fn to_watts(per_unit: f64, model: &proto::NetworkModel) -> f64 {
    per_unit * model.s_base_va
}

/// Multiplies a per-unit voltage back into volts.
#[must_use]
pub fn to_volts(per_unit: f64, model: &proto::NetworkModel) -> f64 {
    per_unit * model.v_base_v
}

/// Applies wire overrides to a case, in the order given.
///
/// Later overrides win over earlier ones on the same element, which the
/// contract states. Out-of-range indices are ignored rather than rejected: an
/// override naming a branch that does not exist is a client bug, but failing
/// the whole sweep over one is worse than solving the network that was
/// actually described.
pub fn apply_overrides(case: &mut Case, overrides: &[proto::Override]) {
    for wire in overrides {
        match &wire.change {
            Some(proto::r#override::Change::BranchStatus(status)) => {
                if let Some(branch) = case.branches.get_mut(status.branch as usize) {
                    branch.in_service = status.in_service;
                }
            }
            Some(proto::r#override::Change::BusInjection(injection)) => {
                let s_base = case.base.s_base();
                if let Some(bus) = case.buses.get_mut(injection.bus as usize) {
                    bus.p_injection = injection.p_injection_w / s_base;
                    bus.q_injection = injection.q_injection_var / s_base;
                }
            }
            // Normalized by the same `/ s_base` the model's own `rating_va`
            // goes through above, so a rating that arrives as an override and
            // the same rating baked into the uploaded model are the same
            // number here.
            //
            // Zero passes through untouched because zero is what
            // `Branch::rating` already means by unrated — and unrated is
            // *skipped* by every loading check downstream, not treated as a
            // limit of nothing. An override of zero therefore hides a
            // violation rather than manufacturing one, which is why the
            // contract tells producers to send no override instead.
            Some(proto::r#override::Change::BranchRating(rating)) => {
                let s_base = case.base.s_base();
                if let Some(branch) = case.branches.get_mut(rating.branch as usize) {
                    branch.rating = rating.rating_va / s_base;
                }
            }
            // `None` is not only an override that set no change. prost decodes
            // an unrecognized oneof member into `None`, so this arm is also
            // every override a NEWER caller sends that this binary does not
            // know about — and it discards them without a word.
            //
            // That is how a rolling deploy misleads: a caller that sends
            // `BranchRating` to a pod built before member 3 existed gets a
            // clean response computed entirely on the ratings in the uploaded
            // model, with nothing in it to say the weather never arrived.
            //
            // It cannot be fixed by failing here — this binary genuinely
            // cannot tell an empty override from a future one, and rejecting
            // both would refuse the empty case the field's own absence
            // describes. It needs the response to state how many overrides
            // were applied, so a caller can compare that against what it sent.
            // Until then callers cannot detect the skew either; the scenario
            // extension documents the same hole from its end, at the
            // `SolverVersion` assignment in `extensions/scenario/execute.go`.
            None => {}
        }
    }
}

/// Converts one branch's overcurrent element.
///
/// Absent means no overcurrent element at all — a branch protected by
/// distance only must not gain a phantom one merely because the message has
/// a field for it. A pickup of zero can never trip (`Overcurrent::time_to_trip`
/// treats it as unrated), which is what "absent" is built out of here.
///
/// A *present* message's own zero-valued fields default to the physical
/// convention documented on the wire: a relay set at its own branch's
/// rating.
fn to_overcurrent(wire: Option<&proto::OvercurrentSettings>) -> Overcurrent {
    let Some(wire) = wire else {
        return Overcurrent {
            pickup: 0.0,
            ..Overcurrent::default()
        };
    };

    let default = Overcurrent::default();

    Overcurrent {
        pickup: if wire.pickup > 0.0 {
            wire.pickup
        } else {
            default.pickup
        },
        time_multiplier: if wire.time_multiplier > 0.0 {
            wire.time_multiplier
        } else {
            default.time_multiplier
        },
        // Absent disables the instantaneous element outright — a multiple of
        // zero would trip on any flow, which the wire message's own comment
        // says is a different and much more aggressive setting.
        instantaneous: wire.instantaneous,
        curve: to_curve(wire.curve),
        breaker_time: if wire.breaker_time_s > 0.0 {
            wire.breaker_time_s
        } else {
            default.breaker_time
        },
    }
}

/// Maps the wire curve, treating `CURVE_UNSPECIFIED` the same as every other
/// zero-valued field on `OvercurrentSettings`: the server's default, which is
/// `StandardInverse` — "the usual transmission choice", per the curve's own
/// doc on both sides of the wire.
fn to_curve(wire: i32) -> Curve {
    match proto::Curve::try_from(wire) {
        Ok(proto::Curve::VeryInverse) => Curve::VeryInverse,
        Ok(proto::Curve::ExtremelyInverse) => Curve::ExtremelyInverse,
        Ok(proto::Curve::StandardInverse | proto::Curve::Unspecified) | Err(_) => {
            Curve::StandardInverse
        }
    }
}

/// Converts one branch's distance element. Absent means no distance zones at
/// all — see [`to_overcurrent`] for the same reasoning applied to the other
/// element.
fn to_distance(wire: Option<&proto::DistanceSettings>) -> Distance {
    let Some(wire) = wire else {
        return Distance {
            zones: Vec::new(),
            breaker_time: 0.0,
        };
    };

    let default = Distance::default();

    Distance {
        zones: wire
            .zones
            .iter()
            .map(|zone| Zone {
                reach: zone.reach,
                delay: zone.delay_s,
            })
            .collect(),
        breaker_time: if wire.breaker_time_s > 0.0 {
            wire.breaker_time_s
        } else {
            default.breaker_time
        },
    }
}

/// Converts one branch's (or the network default's) protection settings.
fn to_settings(wire: &proto::ProtectionSettings) -> Settings {
    Settings {
        overcurrent: to_overcurrent(wire.overcurrent.as_ref()),
        distance: to_distance(wire.distance.as_ref()),
    }
}

/// Builds the branch protection resolver from the request's network-wide
/// default and per-branch overrides.
///
/// A wholly absent `protection` message — the caller supplied nothing at all
/// — reads as the physical-convention default for BOTH elements, matching
/// the wire comment on `SimulateCascadeRequest.protection`: "zero-valued
/// fields default server-side to a relay set at its own branch's rating."
/// That is a different question from a *present* message whose `overcurrent`
/// or `distance` is itself absent, which is the caller deliberately
/// disabling one element — see [`to_overcurrent`]/[`to_distance`]. Collapsing
/// the two would either silently disable protection nobody asked to turn
/// off, or silently defeat a caller's explicit "distance only".
#[must_use]
pub fn to_protection(
    defaults: Option<&proto::ProtectionSettings>,
    branch_protection: &[proto::BranchProtection],
) -> Protection {
    let defaults = defaults.map_or_else(
        || Settings {
            overcurrent: Overcurrent::default(),
            distance: Distance::default(),
        },
        to_settings,
    );

    let overrides = branch_protection
        .iter()
        .filter_map(|wire| {
            wire.settings
                .as_ref()
                .map(|settings| (wire.branch as usize, to_settings(settings)))
        })
        .collect();

    Protection::new(defaults, overrides)
}

/// Converts the wire UFLS scheme.
///
/// Absent means no shedding scheme at all — deliberately with NO server-side
/// default, unlike [`to_protection`] above: a shedding schedule is a
/// utility's policy choice, not a physical convention this server has
/// standing to assert on a caller's behalf. See the wire message's own
/// comment on `SimulateCascadeRequest.ufls`.
///
/// The survival band (`floor_hz`/`ceiling_hz`) is a DIFFERENT question from
/// the stages above, even though both live on the same wire message. The
/// band is physics — where an island is considered to have collapsed — not
/// utility policy, so it gets the same server-side default `Scheme::none()`
/// uses whether or not the caller supplied any shedding stages. proto3 gives
/// a `double` field left unset the same wire representation as an explicit
/// `0.0`; a zero (or non-finite) band is never a real survival band, so it
/// is treated as "the caller didn't set this" and substituted. Without this,
/// a client that sends stages but forgets the band gets `ceiling_hz = 0.0`,
/// every settling frequency reads above it, and a perfectly healthy network
/// is reported `Collapsed`. This does NOT touch `stages`: an empty stage
/// list is left empty, because inventing a shedding stage nobody asked for
/// would be exactly the policy overreach the paragraph above forbids.
#[must_use]
pub fn to_scheme(wire: Option<&proto::UflsScheme>) -> Scheme {
    let Some(wire) = wire else {
        return Scheme::none();
    };

    let defaults = Scheme::none();
    let floor_hz = if wire.floor_hz.is_finite() && wire.floor_hz != 0.0 {
        wire.floor_hz
    } else {
        defaults.floor_hz
    };
    let ceiling_hz = if wire.ceiling_hz.is_finite() && wire.ceiling_hz != 0.0 {
        wire.ceiling_hz
    } else {
        defaults.ceiling_hz
    };

    Scheme {
        stages: wire
            .stages
            .iter()
            .map(|stage| Stage {
                at_hz: stage.at_hz,
                shed_fraction: stage.shed_fraction,
                delay: stage.delay_s,
            })
            .collect(),
        floor_hz,
        ceiling_hz,
    }
}

#[cfg(test)]
// These assertions compare a field this module passed through verbatim
// against the literal it was given — no arithmetic happens in between, so
// exact equality is the correct assertion and an epsilon would only hide a
// conversion that silently rewrote its input. Matches the same reasoning
// `bs-case`'s own constructor tests document.
#[allow(clippy::float_cmp)]
mod tests {
    use super::{
        apply_overrides, to_case, to_curve, to_distance, to_overcurrent, to_protection, to_scheme,
    };
    use crate::proto;

    /// A minimal one-bus, zero-branch model, so `to_case` has something to
    /// convert without every test needing to restate a whole network.
    fn minimal_model(bus: proto::Bus) -> proto::NetworkModel {
        proto::NetworkModel {
            name: "t".to_owned(),
            s_base_va: 100e6,
            v_base_v: 138_000.0,
            buses: vec![bus],
            branches: vec![],
        }
    }

    fn slack_bus() -> proto::Bus {
        proto::Bus {
            id: "s".to_owned(),
            kind: proto::BusKind::Slack as i32,
            p_injection_w: 0.0,
            q_injection_var: 0.0,
            v_magnitude_v: 138_000.0,
            v_angle_rad: 0.0,
            q_min_var: None,
            q_max_var: None,
            g_shunt_s: 0.0,
            b_shunt_s: 0.0,
            p_min_w: None,
            p_max_w: None,
            inertia_h_s: None,
            droop_r: None,
            v_nominal_v: None,
        }
    }

    /// Absent `p_min_w`/`p_max_w` means unbounded, matching the reactive
    /// limits' existing convention — not zero, which would make every
    /// generator with no stated limit unable to produce anything.
    #[test]
    fn absent_real_limits_are_unbounded() {
        let case = to_case(&minimal_model(slack_bus())).expect("a minimal model converts");

        assert_eq!(case.buses[0].p_min, f64::NEG_INFINITY);
        assert_eq!(case.buses[0].p_max, f64::INFINITY);
    }

    /// Inertia (seconds) and droop (dimensionless) pass through unconverted —
    /// dividing either by `s_base_va` would be the mirror image of forgetting
    /// to divide a power.
    #[test]
    fn inertia_and_droop_are_not_divided_by_the_base() {
        let bus = proto::Bus {
            inertia_h_s: Some(5.0),
            droop_r: Some(0.05),
            ..slack_bus()
        };
        let case = to_case(&minimal_model(bus)).expect("a minimal model converts");

        assert_eq!(case.buses[0].inertia_h, Some(5.0));
        assert_eq!(case.buses[0].droop_r, Some(0.05));
    }

    /// A bus's own nominal voltage is informational and must stay out of the
    /// per-unit conversion. The base is per model, one per zone, and a
    /// transformer already puts each end of a branch in a different zone —
    /// so dividing a bus's values by its own nominal voltage would not be a
    /// sharper conversion, it would be a second and incoherent per-unit
    /// system reached by accident.
    ///
    /// The value chosen is deliberately absurd: at 13.8 kV against the
    /// model's 138 kV, a conversion that read it would put the slack's
    /// voltage at 10.0 per unit rather than 1.0.
    #[test]
    fn a_buses_nominal_voltage_does_not_enter_the_per_unit_conversion() {
        let plain = to_case(&minimal_model(slack_bus())).expect("a minimal model converts");

        let stated = proto::Bus {
            v_nominal_v: Some(13_800.0),
            ..slack_bus()
        };
        let stated = to_case(&minimal_model(stated)).expect("a minimal model converts");

        assert_eq!(stated.buses[0].v_magnitude, plain.buses[0].v_magnitude);
        assert_eq!(stated.buses[0].v_magnitude, 1.0);
        assert_eq!(stated.base.v_base(), plain.base.v_base());
    }

    /// An absent `OvercurrentSettings` disables the element outright: a
    /// pickup of zero, which never trips, rather than the rating-based
    /// default a present-but-zero-valued message would get.
    #[test]
    fn absent_overcurrent_is_disabled_not_defaulted() {
        let overcurrent = to_overcurrent(None);
        assert_eq!(overcurrent.pickup, 0.0);
    }

    /// A present `OvercurrentSettings` with every scalar field zero-valued
    /// reads as the physical convention: pickup at rating.
    #[test]
    fn zero_valued_overcurrent_fields_use_the_physical_default() {
        let wire = proto::OvercurrentSettings {
            pickup: 0.0,
            time_multiplier: 0.0,
            instantaneous: Some(2.5),
            curve: proto::Curve::Unspecified as i32,
            breaker_time_s: 0.0,
        };
        let overcurrent = to_overcurrent(Some(&wire));

        assert_eq!(overcurrent, bs_protection::Overcurrent::default());
    }

    /// `CURVE_UNSPECIFIED` reads as the same default the rest of the message
    /// already applies to its zero-valued fields — `StandardInverse`.
    #[test]
    fn unspecified_curve_defaults_to_standard_inverse() {
        assert_eq!(
            to_curve(proto::Curve::Unspecified as i32),
            bs_protection::Curve::StandardInverse
        );
    }

    /// An absent `DistanceSettings` disables the element: no zones, so
    /// nothing can ever see a fault through it.
    #[test]
    fn absent_distance_has_no_zones() {
        let distance = to_distance(None);
        assert!(distance.zones.is_empty());
    }

    /// A wholly absent `protection` message — the caller sent nothing —
    /// reads as the physical-convention default for both elements, matching
    /// the wire's own comment on `SimulateCascadeRequest.protection`.
    #[test]
    fn absent_protection_defaults_to_the_physical_convention() {
        let protection = to_protection(None, &[]);
        let settings = protection.for_branch(0);

        assert_eq!(settings.overcurrent, bs_protection::Overcurrent::default());
        assert_eq!(settings.distance, bs_protection::Distance::default());
    }

    /// A per-branch override wins over the network default for its own
    /// branch, and leaves every other branch on the default.
    #[test]
    fn a_branch_override_applies_only_to_its_own_branch() {
        let protection = to_protection(
            None,
            &[proto::BranchProtection {
                branch: 2,
                settings: Some(proto::ProtectionSettings {
                    overcurrent: None,
                    distance: None,
                }),
            }],
        );

        assert_eq!(protection.for_branch(2).overcurrent.pickup, 0.0);
        assert_eq!(protection.for_branch(0).overcurrent.pickup, 1.0);
    }

    /// Absent means no shedding scheme at all — no server-side substitute.
    #[test]
    fn absent_ufls_scheme_is_none() {
        assert_eq!(to_scheme(None), bs_frequency::Scheme::none());
    }

    /// A present scheme's frequencies, fractions and delays pass through
    /// unconverted: hertz stays hertz, a fraction stays a fraction, seconds
    /// stay seconds.
    #[test]
    fn a_present_scheme_carries_its_values_through_unconverted() {
        let wire = proto::UflsScheme {
            stages: vec![proto::UflsStage {
                at_hz: 59.3,
                shed_fraction: 0.1,
                delay_s: 0.3,
            }],
            floor_hz: 57.0,
            ceiling_hz: 61.8,
        };

        let scheme = to_scheme(Some(&wire));

        assert_eq!(scheme.floor_hz, 57.0);
        assert_eq!(scheme.ceiling_hz, 61.8);
        assert_eq!(scheme.stages.len(), 1);
        assert_eq!(scheme.stages[0].at_hz, 59.3);
        assert_eq!(scheme.stages[0].shed_fraction, 0.1);
        assert_eq!(scheme.stages[0].delay, 0.3);
    }

    /// A present scheme with stages but an unset survival band gets the same
    /// default band `Scheme::none()` uses — proto3 gives an unset `double`
    /// the same wire form as an explicit `0.0`, and a `ceiling_hz` of `0.0`
    /// would classify every settling frequency as `Collapsed` (see
    /// `bs_frequency::ufls::classify`). The stages themselves are left
    /// exactly as given, empty or not: this substitution is about the band
    /// alone, never about inventing a shedding stage.
    ///
    /// What would make this fail: `to_scheme` copying `wire.floor_hz`/
    /// `wire.ceiling_hz` straight through, the way it did before this fix.
    #[test]
    fn a_present_scheme_with_an_unset_band_gets_the_default_band() {
        let wire = proto::UflsScheme {
            stages: vec![proto::UflsStage {
                at_hz: 59.5,
                shed_fraction: 0.1,
                delay_s: 0.1,
            }],
            floor_hz: 0.0,
            ceiling_hz: 0.0,
        };

        let scheme = to_scheme(Some(&wire));

        assert_eq!(scheme.floor_hz, bs_frequency::Scheme::none().floor_hz);
        assert_eq!(scheme.ceiling_hz, bs_frequency::Scheme::none().ceiling_hz);
        assert_eq!(
            scheme.stages.len(),
            1,
            "the band substitution must not touch the stages"
        );
    }

    /// A rating override lands in per unit, not in volt-amperes.
    ///
    /// The whole point of the override is that a weather-derived rating and a
    /// rating uploaded inside the model become the same number once they are
    /// in a `Case`. Skipping the `/ s_base` would leave a 478 MVA rating
    /// sitting next to per-unit flows around 1.0, and every branch would read
    /// as comfortably within a limit 478 million times its own flow.
    ///
    /// What would make this fail: `apply_overrides` assigning `rating_va`
    /// directly, or converting with `v_base_v` instead of `s_base_va`.
    #[test]
    fn a_rating_override_is_normalized_against_the_power_base() {
        let mut case = to_case(&two_bus_one_branch()).expect("a two-bus model converts");

        apply_overrides(
            &mut case,
            &[proto::Override {
                change: Some(proto::r#override::Change::BranchRating(
                    proto::BranchRating {
                        branch: 0,
                        rating_va: 478.0e6,
                    },
                )),
            }],
        );

        assert!((case.branches[0].rating - 4.78).abs() < 1e-9);
    }

    /// An override naming a branch the model does not have is ignored, the
    /// same way an out-of-range status override already is: one bad index
    /// from a client must not lose the sweep.
    #[test]
    fn a_rating_override_for_a_missing_branch_is_ignored() {
        let mut case = to_case(&two_bus_one_branch()).expect("a two-bus model converts");
        let before = case.branches[0].rating;

        apply_overrides(
            &mut case,
            &[proto::Override {
                change: Some(proto::r#override::Change::BranchRating(
                    proto::BranchRating {
                        branch: 99,
                        rating_va: 478.0e6,
                    },
                )),
            }],
        );

        assert_eq!(case.branches[0].rating, before);
    }

    /// A slack bus, a load bus, and one line between them, so the override
    /// tests have a branch to aim at.
    fn two_bus_one_branch() -> proto::NetworkModel {
        let load = proto::Bus {
            id: "l".to_owned(),
            kind: proto::BusKind::Pq as i32,
            ..slack_bus()
        };

        proto::NetworkModel {
            branches: vec![proto::Branch {
                id: "ln1".to_owned(),
                from_bus: 0,
                to_bus: 1,
                r_ohm: 1.0,
                x_ohm: 10.0,
                b_shunt_s: 0.0,
                rating_va: 100.0e6,
                in_service: true,
                tap: 1.0,
                shift_rad: 0.0,
            }],
            buses: vec![slack_bus(), load],
            ..minimal_model(slack_bus())
        }
    }
}
