//! Sampling a decoded field at a point on the earth.
//!
//! This is what the whole decoder exists for. A span is a few hundred metres
//! of conductor between two structures, and what a hazard model needs is the
//! gust *there* — not a grid, and not a nearest-cell approximation when the
//! cell is 3 km across and the span sits near its edge.
//!
//! Two things are load-bearing.
//!
//! A point outside the grid returns `None`, never a default. HRRR covers CONUS
//! and nothing else, so a span in Alaska has no HRRR gust — and a zero there
//! would be a fabricated calm entering a hazard calculation with no way for
//! anything downstream to know it was invented.
//!
//! Interpolation is bilinear rather than nearest-neighbour. At 3 km spacing a
//! nearest-cell lookup makes the sampled value jump discontinuously as a route
//! crosses a cell boundary, which would show up as neighbouring spans of one
//! line reporting materially different weather for no physical reason.

use crate::{Grid, Message};

/// The earth radius GRIB2 shape code 6 specifies, in metres.
///
/// A sphere, not an ellipsoid. Both products declare shape 6, so this is their
/// own figure rather than an approximation chosen here.
const EARTH_RADIUS_M: f64 = 6_371_229.0;

/// Scan mode bit 0x40: rows run south to north, so row zero is the southern
/// edge. Clear means the first row is the northern edge, which is what GFS
/// uses.
const SCAN_J_POSITIVE: u8 = 0x40;

impl Grid {
    /// Converts a position to fractional grid coordinates.
    ///
    /// Returns `None` when the point falls outside the grid, which is a real
    /// answer rather than a failure: it is what "HRRR does not cover this
    /// span" looks like.
    #[must_use]
    pub fn locate(&self, lon: f64, lat: f64) -> Option<(f64, f64)> {
        let (i, j) = match *self {
            Self::LatLon {
                la1,
                lo1,
                dlon,
                dlat,
                scan_mode,
                ..
            } => {
                // Longitudes wrap, so the difference is normalised before
                // being divided. Without this a global grid starting at 0
                // would place everything west of Greenwich off its left edge.
                let mut east = lon - lo1;

                while east < 0.0 {
                    east += 360.0;
                }

                while east >= 360.0 {
                    east -= 360.0;
                }

                let i = east / dlon;

                let j = if scan_mode & SCAN_J_POSITIVE == 0 {
                    // Rows run north to south from la1.
                    (la1 - lat) / dlat
                } else {
                    (lat - la1) / dlat
                };

                (i, j)
            }
            Self::Lambert { .. } => self.locate_lambert(lon, lat)?,
        };

        let (nx, ny) = self.shape();

        // A point exactly on the far edge is inside; one beyond it is not. The
        // bilinear read below needs i+1 and j+1, so the usable range stops one
        // cell short of the last index.
        // A grid dimension is a point count, in the thousands — far inside
        // f64's exact-integer range, though the type does not say so.
        #[allow(clippy::cast_precision_loss)]
        let (last_x, last_y) = ((nx as f64) - 1.0, (ny as f64) - 1.0);

        let inside = i >= 0.0 && j >= 0.0 && i <= last_x && j <= last_y;

        inside.then_some((i, j))
    }

    /// The Lambert conformal inverse, from a position to grid coordinates.
    fn locate_lambert(&self, lon: f64, lat: f64) -> Option<(f64, f64)> {
        let Self::Lambert {
            la1,
            lo1,
            lov,
            dx,
            dy,
            latin1,
            latin2,
            ..
        } = *self
        else {
            return None;
        };

        // The cone constant. When the standard parallels coincide the cone is
        // tangent and n is simply sin(latin1) — which is HRRR's case, verified
        // against a real message. The secant form is kept because nothing
        // guarantees the next product is tangent.
        let n = if (latin1 - latin2).abs() < 1e-9 {
            latin1.to_radians().sin()
        } else {
            let top = latin1.to_radians().cos() / latin2.to_radians().cos();
            let bottom = tan_half(latin2) / tan_half(latin1);

            top.ln() / bottom.ln()
        };

        if n.abs() < f64::EPSILON {
            return None;
        }

        let f = latin1.to_radians().cos() * tan_half(latin1).powf(n) / n;

        let rho = |degrees: f64| EARTH_RADIUS_M * f / tan_half(degrees).powf(n);

        // The projection plane's origin sits at the first grid point, so the
        // grid indices are offsets from it rather than from the projection's
        // own centre.
        let rho0 = rho(la1);
        let theta0 = n * angle_difference(lo1, lov).to_radians();
        let x0 = rho0 * theta0.sin();
        let y0 = rho0 - rho0 * theta0.cos();

        let rho_point = rho(lat);
        let theta = n * angle_difference(lon, lov).to_radians();
        let x = rho_point * theta.sin();
        let y = rho0 - rho_point * theta.cos();

        Some(((x - x0) / dx, (y - y0) / dy))
    }
}

/// `tan(pi/4 + lat/2)`, the recurring term in the conformal projections.
fn tan_half(degrees: f64) -> f64 {
    (std::f64::consts::FRAC_PI_4 + degrees.to_radians() / 2.0).tan()
}

/// The signed angle from `to` to `from`, wrapped into ±180.
///
/// Wrapping matters at the date line: a longitude of 179 and a reference of
/// -179 are two degrees apart, not 358, and the unwrapped difference would put
/// the point across the planet from where it is.
fn angle_difference(from: f64, to: f64) -> f64 {
    let mut difference = from - to;

    while difference > 180.0 {
        difference -= 360.0;
    }

    while difference < -180.0 {
        difference += 360.0;
    }

    difference
}

impl Message {
    /// Samples the field at a position, interpolating bilinearly.
    ///
    /// Returns `None` when the position falls outside the grid — see the
    /// module documentation for why that is not a zero.
    #[must_use]
    pub fn sample(&self, lon: f64, lat: f64) -> Option<f64> {
        let (i, j) = self.header.grid.locate(lon, lat)?;
        let (nx, _) = self.header.grid.shape();

        // Truncating toward zero is safe because locate already rejected
        // negatives, and the fractional remainder is the interpolation weight.
        #[allow(clippy::cast_possible_truncation, clippy::cast_sign_loss)]
        let (i0, j0) = (i.trunc() as usize, j.trunc() as usize);

        #[allow(clippy::cast_precision_loss)]
        let (fi, fj) = (i - (i0 as f64), j - (j0 as f64));

        // Clamping the upper neighbour handles a point exactly on the far
        // edge, where i0 is already the last index and the weight is zero.
        let (columns, rows) = self.header.grid.shape();
        let i1 = (i0 + 1).min(columns - 1);
        let j1 = (j0 + 1).min(rows - 1);

        let at = |x: usize, y: usize| self.values.get(y * nx + x).copied();

        let v00 = at(i0, j0)?;
        let v10 = at(i1, j0)?;
        let v01 = at(i0, j1)?;
        let v11 = at(i1, j1)?;

        let bottom = v00 * (1.0 - fi) + v10 * fi;
        let top = v01 * (1.0 - fi) + v11 * fi;

        Some(bottom * (1.0 - fj) + top * fj)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn hrrr() -> Grid {
        // The real HRRR grid, read from a message's section 3.
        Grid::Lambert {
            nx: 1799,
            ny: 1059,
            la1: 21.138_123,
            lo1: -122.719_528,
            lad: 38.5,
            lov: -97.5,
            dx: 3000.0,
            dy: 3000.0,
            latin1: 38.5,
            latin2: 38.5,
            scan_mode: 0x40,
        }
    }

    #[test]
    fn the_first_grid_point_is_the_origin() {
        let (i, j) = hrrr().locate(-122.719_528, 21.138_123).expect("in domain");

        assert!(
            i.abs() < 1e-6 && j.abs() < 1e-6,
            "origin landed at {i}, {j}"
        );
    }

    /// A point well inside CONUS lands well inside the grid. Austin is roughly
    /// central, so a projection error of any size shows up here.
    #[test]
    fn austin_is_inside_conus() {
        let (i, j) = hrrr().locate(-97.67, 30.19).expect("Austin is in CONUS");

        assert!(
            i > 800.0 && i < 1100.0,
            "Austin should be near the middle in x, got {i}"
        );
        assert!(j > 200.0 && j < 500.0, "Austin's y index: {j}");
    }

    /// The reference meridian passes through the grid, and a point on it must
    /// have no rotation — which is the check that catches a sign error in the
    /// cone constant.
    #[test]
    fn a_point_on_the_reference_meridian_has_no_x_rotation() {
        let grid = hrrr();

        let (i_south, _) = grid.locate(-97.5, 25.0).expect("in domain");
        let (i_north, _) = grid.locate(-97.5, 45.0).expect("in domain");

        assert!(
            (i_south - i_north).abs() < 1.0,
            "points on the reference meridian drifted in x: {i_south} vs {i_north}"
        );
    }

    /// Latitude increases with the row index under scan mode 0x40. Getting
    /// this backwards flips the field top to bottom, which no single spot
    /// check would catch.
    #[test]
    fn rows_run_south_to_north() {
        let grid = hrrr();

        let (_, j_south) = grid.locate(-97.5, 25.0).expect("in domain");
        let (_, j_north) = grid.locate(-97.5, 45.0).expect("in domain");

        assert!(
            j_north > j_south,
            "scan mode 0x40 means +j north, got {j_south} then {j_north}"
        );
    }

    #[test]
    fn points_outside_conus_are_absent() {
        let grid = hrrr();

        for (name, lon, lat) in [
            ("the Gulf of Guinea", 0.0, 0.0),
            ("London", -0.13, 51.5),
            ("Tokyo", 139.7, 35.7),
            ("the south pole", 0.0, -89.0),
        ] {
            assert!(
                grid.locate(lon, lat).is_none(),
                "{name} reported as inside the HRRR domain"
            );
        }
    }

    #[test]
    fn a_global_latlon_grid_covers_everything() {
        // GFS: 0.25 degrees, first point at the north pole, rows running south.
        let grid = Grid::LatLon {
            nx: 1440,
            ny: 721,
            la1: 90.0,
            lo1: 0.0,
            dlon: 0.25,
            dlat: 0.25,
            scan_mode: 0,
        };

        let (i, j) = grid.locate(0.0, 90.0).expect("the north pole");

        assert!(i.abs() < 1e-9 && j.abs() < 1e-9, "origin at {i}, {j}");

        let (_, j_south) = grid.locate(0.0, -90.0).expect("the south pole");

        assert!(
            (j_south - 720.0).abs() < 1e-6,
            "the south pole should be the last row, got {j_south}"
        );

        // A longitude west of the prime meridian wraps rather than falling off
        // the left edge.
        let (i_west, _) = grid.locate(-1.0, 0.0).expect("just west of Greenwich");

        assert!(
            (i_west - 1436.0).abs() < 1e-6,
            "westward wrap put it at {i_west}"
        );
    }

    #[test]
    fn the_date_line_does_not_wrap_the_wrong_way() {
        // 179 relative to a reference at -179 is two degrees *west*, so the
        // offset is negative. The unwrapped difference is 358, which would
        // put the point most of the way around the planet.
        assert!((angle_difference(179.0, -179.0) + 2.0).abs() < 1e-9);
        assert!((angle_difference(-179.0, 179.0) - 2.0).abs() < 1e-9);
        assert!((angle_difference(10.0, 5.0) - 5.0).abs() < 1e-9);
    }
}
