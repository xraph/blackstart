//! Factorization: the seam, and the in-tree implementation.

use crate::{Csr, Error};

/// A linear solver that can be factorized once and reused.
///
/// The split matters more here than in most numerical code. Contingency
/// analysis solves the same network hundreds of times with one branch removed,
/// and the sparsity pattern is unchanged between solves — so the expensive
/// symbolic work is done once and only the numeric factorization repeats. A
/// solver API that only offered "solve this system" would throw that away.
pub trait Factorize {
    /// The reusable factorization.
    type Factors;

    /// Factorizes a square matrix.
    ///
    /// # Errors
    ///
    /// Returns [`Error::NotSquare`] or [`Error::Singular`].
    fn factor(&self, a: &Csr) -> Result<Self::Factors, Error>;

    /// Solves `A x = b` using an existing factorization.
    ///
    /// # Errors
    ///
    /// Returns [`Error::DimensionMismatch`] when `b` is the wrong length.
    fn solve(&self, factors: &Self::Factors, b: &[f64]) -> Result<Vec<f64>, Error>;
}

/// Dense LU with partial pivoting.
///
/// This is the in-tree default, and the choice is license-driven as much as
/// technical. `SuiteSparse` `UMFPACK` is GPL-family and cannot be linked into an
/// Apache-2.0 release. KLU is LGPL and algorithmically ideal for circuit
/// matrices, so it may only ever sit behind a feature flag that is off by
/// default. A dense factorization has no such problem, is exact, and is
/// entirely adequate for the IEEE cases the solver is validated against —
/// case300 is a 300×300 system, which is microseconds.
///
/// It is not adequate for ACTIVSg-2000, and that is what the trait is for.
#[derive(Debug, Clone, Copy, Default)]
pub struct DenseLu;

/// An LU factorization with its pivot sequence.
#[derive(Debug, Clone)]
pub struct DenseFactors {
    n: usize,
    /// Combined L and U. L's unit diagonal is implicit.
    lu: Vec<f64>,
    /// Row `i` of the factorization came from row `perm[i]` of the input.
    perm: Vec<usize>,
}

impl DenseFactors {
    /// Dimension of the factorized system.
    #[must_use]
    pub const fn dim(&self) -> usize {
        self.n
    }
}

impl Factorize for DenseLu {
    type Factors = DenseFactors;

    fn factor(&self, a: &Csr) -> Result<Self::Factors, Error> {
        if !a.is_square() {
            return Err(Error::NotSquare {
                rows: a.rows(),
                cols: a.cols(),
            });
        }

        let n = a.rows();
        let mut lu = a.to_dense();
        let mut perm: Vec<usize> = (0..n).collect();

        for k in 0..n {
            // Partial pivoting. Without it a zero on the diagonal stops the
            // factorization even when the matrix is perfectly well-conditioned,
            // and power-flow matrices routinely have zero diagonals before
            // ordering.
            let mut pivot_row = k;
            let mut pivot_mag = lu[k * n + k].abs();

            for row in k + 1..n {
                let mag = lu[row * n + k].abs();
                if mag > pivot_mag {
                    pivot_mag = mag;
                    pivot_row = row;
                }
            }

            if pivot_mag <= f64::EPSILON {
                return Err(Error::Singular { pivot: k });
            }

            if pivot_row != k {
                for col in 0..n {
                    lu.swap(k * n + col, pivot_row * n + col);
                }
                perm.swap(k, pivot_row);
            }

            let pivot = lu[k * n + k];

            for row in k + 1..n {
                let factor = lu[row * n + k] / pivot;
                lu[row * n + k] = factor;

                if factor == 0.0 {
                    continue;
                }

                for col in k + 1..n {
                    lu[row * n + col] -= factor * lu[k * n + col];
                }
            }
        }

        Ok(DenseFactors { n, lu, perm })
    }

    fn solve(&self, factors: &Self::Factors, b: &[f64]) -> Result<Vec<f64>, Error> {
        let n = factors.n;

        if b.len() != n {
            return Err(Error::DimensionMismatch {
                expected: n,
                got: b.len(),
            });
        }

        // Apply the row permutation.
        let mut x: Vec<f64> = factors.perm.iter().map(|&p| b[p]).collect();

        // Forward substitution through L, whose diagonal is an implicit 1,
        // then back substitution through U. Both index `x` by the loop
        // variable because that is what substitution is: element `row`
        // depends on the elements already solved. Rewriting it with iterators
        // would hide the recurrence.
        #[allow(clippy::needless_range_loop)]
        for row in 1..n {
            let mut acc = x[row];
            for col in 0..row {
                acc -= factors.lu[row * n + col] * x[col];
            }
            x[row] = acc;
        }

        #[allow(clippy::needless_range_loop)]
        for row in (0..n).rev() {
            let mut acc = x[row];
            for col in row + 1..n {
                acc -= factors.lu[row * n + col] * x[col];
            }
            x[row] = acc / factors.lu[row * n + row];
        }

        Ok(x)
    }
}
