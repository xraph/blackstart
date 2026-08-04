//! Cascading failure simulation.
//!
//! N-1 answers what one outage overloads. This answers what that overload
//! triggers next: protection operates, flow redistributes, more protection
//! operates, the network separates, and each island's frequency decides
//! whether it survives.
//!
//! Islanding is not a feature layered on top. `Case::validate` requires
//! exactly one slack bus and `Reduction::build` deletes exactly one row, so the
//! moment a cascade separates the network every existing solve path is
//! singular. That is why `island` comes first here.
//!
//! Advisory only. See `docs/SAFETY.md`.

#![forbid(unsafe_code)]

pub mod dispatch;
pub mod island;
pub mod step;

pub use dispatch::{Balance, Dispatchable, Plan, apply, rebalance};
pub use island::{Islands, Subcase};
pub use step::{Options, Outcome, StepError, step};
