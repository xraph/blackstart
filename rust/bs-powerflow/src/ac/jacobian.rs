//! The Newton-Raphson Jacobian, in polar coordinates.
//!
//! Four blocks, one per pairing of equation and unknown:
//!
//! ```text
//!        ∂θ    ∂|V|
//! ΔP  [  H      N  ]
//! ΔQ  [  M      L  ]
//! ```
//!
//! with, for `i ≠ j`,
//!
//! ```text
//! H_ij = |V_i||V_j| (G_ij sin θ_ij − B_ij cos θ_ij)
//! N_ij = |V_i|      (G_ij cos θ_ij + B_ij sin θ_ij)
//! M_ij = −|V_i||V_j|(G_ij cos θ_ij + B_ij sin θ_ij)
//! L_ij = |V_i|      (G_ij sin θ_ij − B_ij cos θ_ij)
//! ```
//!
//! and diagonals that fold in the bus's own injection:
//!
//! ```text
//! H_ii = −Q_i − B_ii|V_i|²      N_ii = P_i/|V_i| + G_ii|V_i|
//! M_ii =  P_i − G_ii|V_i|²      L_ii = Q_i/|V_i| − B_ii|V_i|
//! ```
//!
//! The diagonal forms are why [`build`] takes the injections rather than
//! recomputing them: they are already known from the mismatch evaluation of
//! the same iteration, and computing them twice would be both wasteful and a
//! chance for the two to disagree.
//!
//! Note the sign convention. Newton solves `J Δx = −f(x)` for a residual `f`,
//! but the mismatch here is already `scheduled − computed`, which is `−f`. So
//! the step is `J Δx = mismatch` with no negation anywhere, and there is
//! exactly one place — here — where that has to be got right.

use bs_sparse::{Csr, Error as SparseError, Triplets};

use crate::ac::layout::Layout;
use crate::ac::power::{Injections, Voltages};
use crate::ybus::Ybus;

/// Assembles the Jacobian for a voltage state.
///
/// Only the rows and columns belonging to actual unknowns are emitted, so the
/// matrix is square at [`Layout::len`] and the slack's and PV buses' fixed
/// quantities are absent rather than pinned — the same way the DC reduction
/// deletes the slack row instead of constraining it.
///
/// Entries are emitted by walking the admittance sparsity pattern in bus
/// order, so the triplet sequence is identical every run.
///
/// # Errors
///
/// Returns [`SparseError`] if assembly overflows the declared dimensions.
///
/// # Panics
///
/// Panics if the voltage state or injections do not cover every bus.
pub fn build(
    ybus: &Ybus,
    layout: &Layout,
    volts: &Voltages,
    power: &Injections,
) -> Result<Csr, SparseError> {
    assert_eq!(volts.len(), ybus.n_bus(), "voltages must cover every bus");
    assert_eq!(
        power.p.len(),
        ybus.n_bus(),
        "injections must cover every bus"
    );

    let n = layout.len();
    let mut j = Triplets::new(n, n);
    // Four blocks over the same pattern, plus diagonals.
    j.reserve(ybus.g.nnz() * 4);

    for bus in 0..ybus.n_bus() {
        // Rows exist for every non-slack bus (ΔP) and every PQ bus (ΔQ).
        let Some(p_row) = layout.angle_row(bus) else {
            continue;
        };
        let q_row = layout.magnitude_row(bus);

        for ((col, conductance), (col_b, susceptance)) in ybus.g.row(bus).zip(ybus.b.row(bus)) {
            debug_assert_eq!(col, col_b, "G and B must share a sparsity pattern");

            if col == bus {
                // Diagonal terms fold in the injection, so they are written
                // from P_i and Q_i rather than from the sum they came from.
                let magnitude = volts.magnitude[bus];

                j.add(
                    p_row,
                    p_row,
                    -power.q[bus] - susceptance * magnitude * magnitude,
                )?;

                if let Some(col_row) = layout.magnitude_row(bus) {
                    j.add(
                        p_row,
                        col_row,
                        power.p[bus] / magnitude + conductance * magnitude,
                    )?;
                }

                if let Some(q_row) = q_row {
                    j.add(
                        q_row,
                        p_row,
                        power.p[bus] - conductance * magnitude * magnitude,
                    )?;

                    if let Some(col_row) = layout.magnitude_row(bus) {
                        j.add(
                            q_row,
                            col_row,
                            power.q[bus] / magnitude - susceptance * magnitude,
                        )?;
                    }
                }

                continue;
            }

            let (sin, cos) = (volts.angle[bus] - volts.angle[col]).sin_cos();

            // The two recurring groupings. Naming them keeps each block's
            // expression to one line and makes the shared structure visible:
            // H and L use `across`, N and M use `along`.
            let across = conductance.mul_add(sin, -(susceptance * cos));
            let along = conductance.mul_add(cos, susceptance * sin);

            let (mag_i, mag_j) = (volts.magnitude[bus], volts.magnitude[col]);

            if let Some(col_row) = layout.angle_row(col) {
                j.add(p_row, col_row, mag_i * mag_j * across)?;

                if let Some(q_row) = q_row {
                    j.add(q_row, col_row, -mag_i * mag_j * along)?;
                }
            }

            if let Some(col_row) = layout.magnitude_row(col) {
                j.add(p_row, col_row, mag_i * along)?;

                if let Some(q_row) = q_row {
                    j.add(q_row, col_row, mag_i * across)?;
                }
            }
        }
    }

    Ok(j.compress())
}
