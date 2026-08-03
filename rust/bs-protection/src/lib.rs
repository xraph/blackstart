//! Protection models for cascade simulation.
//!
//! Given a branch flow and a set of relay settings, these say whether
//! protection operates and when. Nothing here solves a network — that
//! separation is what lets a disagreement about a trip time be diagnosed in a
//! twenty-line test rather than inside a thirty-step cascade.
//!
//! Advisory only. Nothing here writes to a control system. See `docs/SAFETY.md`.

#![forbid(unsafe_code)]

pub mod distance;
pub mod overcurrent;
pub mod settings;

pub use distance::{Distance, Zone};
pub use overcurrent::{Curve, Overcurrent};
pub use settings::{Protection, Settings, TripCandidate, TripCause, collect};
