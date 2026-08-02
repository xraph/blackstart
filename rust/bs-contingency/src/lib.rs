//! N-1 contingency analysis.
//!
//! The two-tier design this project exists to demonstrate. A DC screen built
//! on line outage distribution factors evaluates every contingency without a
//! factorization, ranks them by how badly loaded the network becomes, and only
//! the worst few are verified by a full AC solve. On a 2000-bus network that
//! is the difference between thousands of factorizations and a handful.
//!
//! The screen is exact for the DC model and approximate for the real network,
//! which is why it ranks rather than reports. A DC overload is a candidate; an
//! AC verification is a finding. Quoting a screen result as a finding is how a
//! linearization ends up presented as an answer, and this crate keeps the two
//! in separate types so that confusing them takes effort.
//!
//! Everything here is advisory. Nothing in this crate — or anywhere in
//! blackstart — writes to a control system. See `docs/SAFETY.md`.

#![forbid(unsafe_code)]

pub mod screen;
pub mod verify;

pub use screen::{Contingency, ScreenOutcome, ScreenResult, Violation, all_branches, rank, screen};
pub use verify::{Verdict, Verification, VoltageLimits, VoltageViolation, verify, verify_top};
