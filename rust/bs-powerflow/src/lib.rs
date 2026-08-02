//! Steady-state power flow.
//!
//! DC power flow is the linearization every contingency screen runs first. It
//! drops losses, holds every voltage at 1.0 per unit, and assumes angle
//! differences are small enough that `sin θ ≈ θ` — which turns the nonlinear
//! power-flow problem into one linear solve. On a transmission network the
//! resulting real-power flows are close enough to be useful, and being linear
//! is what makes distribution factors and fast N-1 screening possible at all.
//!
//! What it cannot tell you is anything about voltage or reactive power, so a
//! violation it reports is a thermal one and a voltage limit needs the AC
//! solution.

#![forbid(unsafe_code)]

pub mod ac;
pub mod dc;
pub mod reduce;
pub mod ybus;

pub use ac::{Layout, Voltages};
pub use dc::{DcSolution, PowerFlowError, branch_flows, solve_dc};
pub use reduce::{ReduceError, Reduction};
pub use ybus::{Ybus, YbusError};
