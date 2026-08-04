//! Island frequency response and under-frequency load shedding.
//!
//! When a cascade separates a network, each island's generation and load no
//! longer match, and its frequency moves. This crate answers what happens
//! next: how fast frequency falls, how deep it goes, which shedding stages
//! operate, and whether the island survives.
//!
//! Nothing here solves a power flow. That separation is what lets a nadir be
//! pinned against a hand computation rather than diagnosed inside a cascade.
//!
//! Advisory only. See `docs/SAFETY.md`.

#![forbid(unsafe_code)]

pub mod integrate;
pub mod machine;
pub mod model;
pub mod ufls;

pub use integrate::{Trajectory, integrate};
pub use machine::{Aggregate, Defaults};
pub use model::{Model, State};
pub use ufls::{IslandCondition, Response, Sample, Scheme, Shed, Stage, respond};
