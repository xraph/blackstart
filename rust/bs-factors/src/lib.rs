//! Linear sensitivity factors for the DC network.
//!
//! These are the whole reason a DC screen of every N-1 contingency is cheap.
//! Because the DC power flow is linear, the effect of a change on every branch
//! flow can be written as a matrix that depends only on the network — not on
//! the operating point — so it is computed once and reused for thousands of
//! cases that would otherwise each need their own solve.
//!
//! Two factors, answering two questions:
//!
//! * [`Ptdf`] — if a megawatt is injected at one bus and withdrawn at another,
//!   how does each branch flow change? Used for transfer analysis and as the
//!   ingredient LODF is built from.
//! * [`Lodf`] — if a branch opens, how does its pre-outage flow redistribute
//!   onto the branches that remain? This is the N-1 screen: one matrix
//!   multiply per contingency instead of one factorization.
//!
//! Both are exact for the DC model. They are *not* exact for the real network,
//! which is why the screen ranks contingencies rather than reporting them: the
//! top-K go on to a full AC verification. A factor that is treated as an answer
//! rather than a ranking is how a linearization ends up quoted as a result.

#![forbid(unsafe_code)]

pub mod lodf;
pub mod ptdf;

pub use lodf::{Lodf, Screenability};
pub use ptdf::{FactorError, Ptdf};
