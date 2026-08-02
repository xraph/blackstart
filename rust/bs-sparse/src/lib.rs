//! Sparse matrix assembly and the factorization seam.
//!
//! Two things live here, and they are separated deliberately.
//!
//! [`Triplets`] accumulates `(row, col, value)` entries and compresses them
//! into [`Csr`]. Assembly is where determinism is won or lost: entries arrive
//! in whatever order the network is walked, duplicates must sum, and the
//! result has to be identical every run. A `HashMap` keyed by `(row, col)`
//! would be the obvious implementation and is banned by `clippy.toml` for
//! exactly this reason — its iteration order varies per process, and once that
//! order reaches a floating-point sum the answer changes in the last bits.
//!
//! [`Factorize`] is the seam a linear solver plugs into. It is a trait rather
//! than a concrete type because the choice is license-driven: `SuiteSparse`
//! UMFPACK is GPL-family and unusable here, KLU is LGPL and may only ever sit
//! behind a feature flag that is off in the shipped build. The in-tree default
//! is a dense LU, which is exact, has no dependency, and is entirely adequate
//! for the IEEE cases the solver is validated against. A sparse implementation
//! slots in behind the same trait when case size demands it.

#![forbid(unsafe_code)]

pub mod lu;

pub use lu::{DenseFactors, DenseLu, Factorize};

use std::fmt;

/// A matrix under construction, as an unordered list of contributions.
///
/// Duplicate coordinates are summed, which is what network assembly needs: a
/// bus's diagonal accumulates a term from every branch incident on it, and
/// each branch contributes independently without knowing the others.
#[derive(Debug, Clone, Default)]
pub struct Triplets {
    rows: usize,
    cols: usize,
    entries: Vec<(usize, usize, f64)>,
}

/// Why a matrix operation failed.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Error {
    /// An index was outside the declared dimensions.
    OutOfBounds { row: usize, col: usize },
    /// The matrix is not square, and the operation requires it.
    NotSquare { rows: usize, cols: usize },
    /// The matrix is singular to working precision.
    ///
    /// In a power-flow context this almost always means the network is
    /// disconnected — an island with no slack bus has no unique solution —
    /// rather than that the arithmetic went wrong.
    Singular { pivot: usize },
    /// A right-hand side did not match the factorization's dimension.
    DimensionMismatch { expected: usize, got: usize },
}

impl fmt::Display for Error {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::OutOfBounds { row, col } => {
                write!(f, "index ({row}, {col}) is outside the matrix")
            }
            Self::NotSquare { rows, cols } => {
                write!(f, "matrix is {rows}x{cols}, expected square")
            }
            Self::Singular { pivot } => write!(
                f,
                "matrix is singular at pivot {pivot}; in a power-flow context \
                 this usually means an island with no slack bus rather than a \
                 numerical failure"
            ),
            Self::DimensionMismatch { expected, got } => {
                write!(f, "right-hand side has length {got}, expected {expected}")
            }
        }
    }
}

impl std::error::Error for Error {}

impl Triplets {
    /// Creates an empty accumulator for a matrix of the given shape.
    #[must_use]
    pub fn new(rows: usize, cols: usize) -> Self {
        Self {
            rows,
            cols,
            entries: Vec::new(),
        }
    }

    /// Reserves space for `n` further entries.
    pub fn reserve(&mut self, n: usize) {
        self.entries.reserve(n);
    }

    /// Adds a contribution. Repeated coordinates sum.
    ///
    /// # Errors
    ///
    /// Returns [`Error::OutOfBounds`] when the index lies outside the declared
    /// shape. Silently growing would hide an indexing mistake in the network
    /// walk, which is the most likely cause.
    pub fn add(&mut self, row: usize, col: usize, value: f64) -> Result<(), Error> {
        if row >= self.rows || col >= self.cols {
            return Err(Error::OutOfBounds { row, col });
        }
        self.entries.push((row, col, value));
        Ok(())
    }

    /// Number of accumulated entries, before duplicates are summed.
    #[must_use]
    pub fn len(&self) -> usize {
        self.entries.len()
    }

    /// Whether anything has been accumulated.
    #[must_use]
    pub fn is_empty(&self) -> bool {
        self.entries.is_empty()
    }

    /// Compresses into row-major sparse form, summing duplicates.
    ///
    /// The sort is by `(row, col)` and is stable, so entries added at the same
    /// coordinate are summed in insertion order. That is what makes the result
    /// byte-identical across runs: floating-point addition is not associative,
    /// so a different summation order is a different matrix in the last bits,
    /// and every downstream result inherits the difference.
    #[must_use]
    pub fn compress(mut self) -> Csr {
        self.entries.sort_by_key(|&(r, c, _)| (r, c));

        let mut values = Vec::with_capacity(self.entries.len());
        let mut col_index = Vec::with_capacity(self.entries.len());
        let mut row_start = vec![0usize; self.rows + 1];

        let mut prev: Option<(usize, usize)> = None;

        for &(r, c, v) in &self.entries {
            if prev == Some((r, c)) {
                // Same coordinate as the previous entry: fold into it rather
                // than emitting a second one.
                if let Some(last) = values.last_mut() {
                    *last += v;
                }
                continue;
            }

            values.push(v);
            col_index.push(c);
            row_start[r + 1] += 1;
            prev = Some((r, c));
        }

        for i in 0..self.rows {
            row_start[i + 1] += row_start[i];
        }

        Csr {
            rows: self.rows,
            cols: self.cols,
            row_start,
            col_index,
            values,
        }
    }
}

/// A matrix in compressed sparse row form.
#[derive(Debug, Clone, PartialEq)]
pub struct Csr {
    rows: usize,
    cols: usize,
    row_start: Vec<usize>,
    col_index: Vec<usize>,
    values: Vec<f64>,
}

impl Csr {
    /// Number of rows.
    #[must_use]
    pub const fn rows(&self) -> usize {
        self.rows
    }

    /// Number of columns.
    #[must_use]
    pub const fn cols(&self) -> usize {
        self.cols
    }

    /// Number of stored entries.
    #[must_use]
    pub fn nnz(&self) -> usize {
        self.values.len()
    }

    /// The stored entries of one row, as `(column, value)` pairs in ascending
    /// column order.
    ///
    /// Assembly that walks a sparsity pattern — the power-flow Jacobian is the
    /// motivating case — needs this. Doing it with `get` instead turns a linear
    /// walk into a binary search per entry, and doing it with `to_dense` throws
    /// away the sparsity that made the matrix worth storing this way.
    ///
    /// # Panics
    ///
    /// Panics if the row index is out of range.
    pub fn row(&self, row: usize) -> impl Iterator<Item = (usize, f64)> + '_ {
        assert!(row < self.rows, "row {row} out of range");

        let (start, end) = (self.row_start[row], self.row_start[row + 1]);

        (start..end).map(move |k| (self.col_index[k], self.values[k]))
    }

    /// Whether the matrix is square.
    #[must_use]
    pub const fn is_square(&self) -> bool {
        self.rows == self.cols
    }

    /// Reads one entry. Absent coordinates are structurally zero.
    #[must_use]
    pub fn get(&self, row: usize, col: usize) -> f64 {
        if row >= self.rows || col >= self.cols {
            return 0.0;
        }

        let (start, end) = (self.row_start[row], self.row_start[row + 1]);

        self.col_index[start..end]
            .binary_search(&col)
            .map_or(0.0, |offset| self.values[start + offset])
    }

    /// Multiplies by a dense vector.
    ///
    /// # Errors
    ///
    /// Returns [`Error::DimensionMismatch`] when `x` does not match the column
    /// count.
    pub fn mul_vec(&self, x: &[f64]) -> Result<Vec<f64>, Error> {
        if x.len() != self.cols {
            return Err(Error::DimensionMismatch {
                expected: self.cols,
                got: x.len(),
            });
        }

        let mut out = vec![0.0; self.rows];

        // The loop variable indexes `out` by construction: row `i` of the
        // product is the dot product of row `i` of the matrix with `x`. An
        // iterator would obscure that correspondence rather than clarify it.
        #[allow(clippy::needless_range_loop)]
        for row in 0..self.rows {
            let (start, end) = (self.row_start[row], self.row_start[row + 1]);
            // Summed in stored column order, which compress() fixed. A
            // different order would change the last bits.
            let mut acc = 0.0;
            for k in start..end {
                acc += self.values[k] * x[self.col_index[k]];
            }
            out[row] = acc;
        }

        Ok(out)
    }

    /// Expands into dense row-major form. For tests and for the dense
    /// factorization; not for large cases.
    #[must_use]
    pub fn to_dense(&self) -> Vec<f64> {
        let mut out = vec![0.0; self.rows * self.cols];

        for row in 0..self.rows {
            let (start, end) = (self.row_start[row], self.row_start[row + 1]);
            for k in start..end {
                out[row * self.cols + self.col_index[k]] = self.values[k];
            }
        }

        out
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn close(got: f64, want: f64, tol: f64) -> bool {
        (got - want).abs() <= tol * want.abs().max(1.0)
    }

    #[test]
    fn a_row_yields_its_stored_entries_in_column_order() {
        let mut t = Triplets::new(3, 3);
        // Added out of order on purpose: compress sorts, and assembly relies
        // on the result being ascending.
        t.add(1, 2, 5.0).unwrap();
        t.add(1, 0, 3.0).unwrap();
        t.add(0, 1, 7.0).unwrap();

        let csr = t.compress();

        assert_eq!(csr.row(1).collect::<Vec<_>>(), vec![(0, 3.0), (2, 5.0)]);
        assert_eq!(csr.row(0).collect::<Vec<_>>(), vec![(1, 7.0)]);
        // An empty row yields nothing rather than a zero, so a walk over the
        // pattern visits only what is stored.
        assert_eq!(csr.row(2).count(), 0);
    }

    #[test]
    fn row_iteration_agrees_with_get() {
        let mut t = Triplets::new(4, 4);
        for (r, c, v) in [(0, 0, 1.0), (0, 3, 2.0), (2, 1, -4.0), (3, 3, 9.0)] {
            t.add(r, c, v).unwrap();
        }

        let csr = t.compress();

        for row in 0..csr.rows() {
            for (col, value) in csr.row(row) {
                assert!(close(value, csr.get(row, col), 1e-15), "({row},{col})");
            }
        }
    }

    #[test]
    fn duplicate_coordinates_sum() {
        let mut t = Triplets::new(2, 2);
        t.add(0, 0, 1.0).unwrap();
        t.add(0, 0, 2.0).unwrap();
        t.add(1, 1, 5.0).unwrap();

        let m = t.compress();

        assert!(close(m.get(0, 0), 3.0, 1e-12));
        assert!(close(m.get(1, 1), 5.0, 1e-12));
        assert_eq!(m.nnz(), 2, "duplicates should fold into one entry");
    }

    /// Network assembly is exactly this: a bus diagonal accumulates a term
    /// from every incident branch, and each branch contributes without
    /// knowing the others.
    #[test]
    fn a_diagonal_accumulates_from_many_contributions() {
        let mut t = Triplets::new(3, 3);
        for _ in 0..10 {
            t.add(1, 1, 0.1).unwrap();
        }

        assert!(close(t.compress().get(1, 1), 1.0, 1e-12));
    }

    /// The property the whole determinism story rests on. Entries arrive in
    /// whatever order the network is walked; the compressed matrix must not
    /// depend on that order beyond the summation order `compress()` fixes.
    #[test]
    fn compression_is_independent_of_insertion_order_for_distinct_entries() {
        let entries = [(0, 1, 2.0), (2, 0, -1.5), (1, 1, 4.0), (0, 0, 3.0)];

        let mut forward = Triplets::new(3, 3);
        for &(r, c, v) in &entries {
            forward.add(r, c, v).unwrap();
        }

        let mut backward = Triplets::new(3, 3);
        for &(r, c, v) in entries.iter().rev() {
            backward.add(r, c, v).unwrap();
        }

        assert_eq!(
            forward.compress(),
            backward.compress(),
            "compressed form depends on insertion order; every downstream \
             result would then vary between runs"
        );
    }

    /// Compressing the same input twice must give the identical structure,
    /// which is what lets a factorization be cached by content.
    #[test]
    fn compression_is_repeatable() {
        let build = || {
            let mut t = Triplets::new(4, 4);
            for i in 0..4 {
                t.add(i, i, 2.0).unwrap();
                if i + 1 < 4 {
                    t.add(i, i + 1, -1.0).unwrap();
                    t.add(i + 1, i, -1.0).unwrap();
                }
            }
            t.compress()
        };

        assert_eq!(build(), build());
    }

    #[test]
    fn out_of_bounds_is_rejected_rather_than_growing() {
        let mut t = Triplets::new(2, 2);
        assert_eq!(t.add(2, 0, 1.0), Err(Error::OutOfBounds { row: 2, col: 0 }));
        assert_eq!(t.add(0, 5, 1.0), Err(Error::OutOfBounds { row: 0, col: 5 }));
        assert!(t.is_empty());
    }

    #[test]
    fn absent_coordinates_read_as_zero() {
        let mut t = Triplets::new(2, 2);
        t.add(0, 0, 1.0).unwrap();
        let m = t.compress();

        assert!(close(m.get(0, 1), 0.0, 1e-12));
        assert!(close(m.get(9, 9), 0.0, 1e-12));
    }

    #[test]
    fn matrix_vector_product() {
        // [2 -1]   [1]   [1]
        // [-1 2] . [1] = [1]
        let mut t = Triplets::new(2, 2);
        t.add(0, 0, 2.0).unwrap();
        t.add(0, 1, -1.0).unwrap();
        t.add(1, 0, -1.0).unwrap();
        t.add(1, 1, 2.0).unwrap();

        let got = t.compress().mul_vec(&[1.0, 1.0]).unwrap();

        assert!(close(got[0], 1.0, 1e-12));
        assert!(close(got[1], 1.0, 1e-12));
    }

    #[test]
    fn mul_vec_checks_dimensions() {
        let m = Triplets::new(2, 2).compress();
        assert_eq!(
            m.mul_vec(&[1.0]),
            Err(Error::DimensionMismatch {
                expected: 2,
                got: 1
            })
        );
    }

    #[test]
    fn dense_expansion() {
        let mut t = Triplets::new(2, 3);
        t.add(0, 0, 1.0).unwrap();
        t.add(1, 2, 4.0).unwrap();

        assert_eq!(t.compress().to_dense(), vec![1.0, 0.0, 0.0, 0.0, 0.0, 4.0]);
    }
}
