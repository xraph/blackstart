//! The power-flow solver service.
//!
//! A gRPC front end over the numerics in `bs-powerflow`, `bs-factors` and
//! `bs-contingency`. It exists because the platform is written in Go and the
//! mathematics is written in Rust, and a process boundary is a cleaner seam
//! between them than a foreign-function interface: the contract is explicit,
//! versioned and lintable, and a solver crash cannot take the platform with it.
//!
//! Two design decisions carry most of the weight, both stated in the contract
//! and both enforced here.
//!
//! Every wire value is SI and the numerics are entirely per unit, so
//! conversion happens in exactly one module — [`convert`]. Scattering a
//! division by a base across the service would mean every new field is a
//! chance to forget one, and a forgotten conversion does not fail. It produces
//! a plausible answer on the wrong scale.
//!
//! Models are uploaded once and referenced by content hash. A sweep solves one
//! network thousands of times, and re-sending 2000 buses each time would make
//! the transport cost more than the arithmetic.
//!
//! ADVISORY ONLY. No method here accepts a setpoint, a breaker command, or
//! anything else a device could act on. See `docs/SAFETY.md`.

#![forbid(unsafe_code)]

/// The generated protobuf bindings.
///
/// Generated into `OUT_DIR` rather than committed, unlike the Go side: Go's
/// tooling expects checked-in stubs and Cargo's does not, and following each
/// ecosystem's convention beats imposing one on both. The contract in `proto/`
/// is the single source either way.
// Lints are switched off wholesale inside, and only inside, this module.
// prost and tonic write this code; it is regenerated on every build and
// nothing here is ours to fix. A narrower allow-list would need extending
// every time either crate changes its output, and the first response to that
// churn would be to widen it anyway.
#[allow(clippy::all, clippy::pedantic, clippy::nursery, missing_docs)]
pub mod proto {
    tonic::include_proto!("blackstart.solver.v1");
}

/// The generated bindings for the weather grid contract.
///
/// A second module because it is a second protobuf package. The two contracts
/// share this binary and nothing else: one is a power-flow service whose every
/// wire value is SI, the other decodes a file format.
#[allow(clippy::all, clippy::pedantic, clippy::nursery, missing_docs)]
pub mod weather_proto {
    tonic::include_proto!("blackstart.weather.v1");
}

pub mod cache;
pub mod convert;
pub mod service;
pub mod weather;

pub use cache::{ModelCache, content_hash};
pub use service::{SOLVER_VERSION, Solver};
pub use weather::{GridCache, WeatherGrid};
