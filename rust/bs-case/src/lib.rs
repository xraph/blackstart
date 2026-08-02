//! The solvable network case.
//!
//! A [`Case`] is what the solver actually operates on: buses indexed
//! contiguously from zero, branches referencing those indices, and everything
//! already in per-unit on one system base. It is deliberately not the platform
//! domain model — that carries names, geometry, ratings and provenance, none
//! of which the numerics need, and all of which would have to be threaded
//! through every inner loop.
//!
//! Index order is load-bearing. It comes from the canonical encoding on the Go
//! side, which sorts entities by identifier before serializing, so the same
//! network always produces the same matrix ordering and therefore the same
//! floating-point summation order. See `a private design note`.

#![forbid(unsafe_code)]

pub mod element;

pub use element::{Branch, Bus, BusKind};
