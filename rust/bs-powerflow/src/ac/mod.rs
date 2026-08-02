//! The AC power flow.
//!
//! Where the DC solve drops losses and holds every voltage at 1.0 per unit,
//! this solves the full nonlinear equations by Newton-Raphson: real and
//! reactive power at every bus, actual voltage magnitudes, and therefore
//! actual losses and voltage violations.
//!
//! It is the second tier of the N-1 design. The DC screen ranks thousands of
//! contingencies cheaply; only the worst few are verified here, because each
//! verification costs a factorization per iteration.

pub mod jacobian;
pub mod layout;
pub mod newton;
pub mod power;

pub use layout::Layout;
pub use newton::{AcError, AcSolution, Options, solve_ac, solve_ac_from};
pub use power::{Injections, Voltages, injections, max_mismatch, mismatch};
