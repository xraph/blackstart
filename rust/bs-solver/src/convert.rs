//! Converting between the SI wire format and the per-unit solver.
//!
//! This module is where per-unit conversion happens, and it is the only place
//! in the crate that does it. The contract says every wire value is SI; the
//! numerics work entirely in per unit. Scattering the division by a base
//! across the service would mean every new field is a chance to forget one,
//! and a forgotten conversion does not fail — it produces a plausible answer
//! on the wrong scale.

use bs_case::{Branch, Bus, BusKind, Case};
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
            None => {}
        }
    }
}
