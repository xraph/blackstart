//! Section 3: where the values sit on the earth.

use crate::GribError;
use crate::section::{be_u16, be_u32, signed_u32};

/// The scale GRIB2 stores angles at: microdegrees.
const ANGLE_SCALE: f64 = 1e6;

/// The scale GRIB2 stores Lambert grid increments at: micrometres.
const LENGTH_SCALE: f64 = 1e6;

/// Where the values sit.
///
/// Two variants, because two are what HRRR and GFS use. A third would be a
/// third variant here and a third arm in the sampler, which is the right
/// amount of work for a real need and too much for a speculative one.
#[derive(Debug, Clone, Copy, PartialEq)]
pub enum Grid {
    /// Template 3.0: a regular latitude/longitude grid. GFS.
    LatLon {
        /// Points along a parallel.
        nx: usize,
        /// Points along a meridian.
        ny: usize,
        /// Latitude of the first grid point, degrees north.
        la1: f64,
        /// Longitude of the first grid point, degrees east in ±180.
        lo1: f64,
        /// Longitude increment, degrees.
        dlon: f64,
        /// Latitude increment, degrees. Always positive; `scan_mode` says
        /// which way the rows run.
        dlat: f64,
        /// WMO code table 3.4 scanning mode flags.
        scan_mode: u8,
    },

    /// Template 3.30: Lambert conformal conic. HRRR.
    Lambert {
        /// Points along the x axis.
        nx: usize,
        /// Points along the y axis.
        ny: usize,
        /// Latitude of the first grid point, degrees north.
        la1: f64,
        /// Longitude of the first grid point, degrees east in ±180.
        lo1: f64,
        /// The latitude at which the projection's increments are exact.
        lad: f64,
        /// The meridian parallel to the y axis, degrees east in ±180.
        lov: f64,
        /// Grid increment along x, metres.
        dx: f64,
        /// Grid increment along y, metres.
        dy: f64,
        /// First standard parallel.
        latin1: f64,
        /// Second standard parallel. Equal to `latin1` for a tangent cone,
        /// which is what HRRR uses.
        latin2: f64,
        /// WMO code table 3.4 scanning mode flags.
        scan_mode: u8,
    },
}

impl Grid {
    /// How many values a message on this grid carries.
    #[must_use]
    pub fn point_count(&self) -> usize {
        match *self {
            Self::LatLon { nx, ny, .. } | Self::Lambert { nx, ny, .. } => nx * ny,
        }
    }

    /// The grid's dimensions, x then y.
    #[must_use]
    pub fn shape(&self) -> (usize, usize) {
        match *self {
            Self::LatLon { nx, ny, .. } | Self::Lambert { nx, ny, .. } => (nx, ny),
        }
    }

    /// The scanning mode flags, which say which way the rows and columns run.
    #[must_use]
    pub fn scan_mode(&self) -> u8 {
        match *self {
            Self::LatLon { scan_mode, .. } | Self::Lambert { scan_mode, .. } => scan_mode,
        }
    }
}

/// Normalises a longitude into ±180.
///
/// GRIB2 stores longitudes as degrees east in 0..360, so HRRR's reference
/// meridian arrives as 262.5 rather than -97.5. Everything downstream — the
/// geometry package, the spans, the client — works in ±180, and mixing the two
/// conventions puts a CONUS grid on the far side of the planet.
fn normalise_longitude(degrees: f64) -> f64 {
    if degrees > 180.0 {
        degrees - 360.0
    } else {
        degrees
    }
}

/// Offsets within section 3, quoted from octet 1 as the WMO tables do.
const TEMPLATE_NUMBER_AT: usize = 12;
const LATLON_MIN_LEN: usize = 72;
const LAMBERT_MIN_LEN: usize = 81;

/// Reads section 3.
///
/// # Errors
///
/// Returns [`GribError::UnsupportedGridTemplate`] for anything but 3.0 and
/// 3.30, and [`GribError::Truncated`] when the section is shorter than its
/// template requires.
pub(crate) fn parse(body: &[u8]) -> Result<Grid, GribError> {
    if body.len() < TEMPLATE_NUMBER_AT + 2 {
        return Err(GribError::Truncated {
            what: "the grid definition template number",
            needed: TEMPLATE_NUMBER_AT + 2,
            available: body.len(),
        });
    }

    let template = be_u16(body, TEMPLATE_NUMBER_AT);

    match template {
        0 => parse_latlon(body),
        30 => parse_lambert(body),
        other => Err(GribError::UnsupportedGridTemplate(other)),
    }
}

fn parse_latlon(body: &[u8]) -> Result<Grid, GribError> {
    if body.len() < LATLON_MIN_LEN {
        return Err(GribError::Truncated {
            what: "a template 3.0 grid definition",
            needed: LATLON_MIN_LEN,
            available: body.len(),
        });
    }

    let nx = be_u32(body, 30) as usize;
    let ny = be_u32(body, 34) as usize;

    let la1 = f64::from(signed_u32(body, 46)) / ANGLE_SCALE;
    let lo1 = f64::from(be_u32(body, 50)) / ANGLE_SCALE;
    let dlon = f64::from(be_u32(body, 63)) / ANGLE_SCALE;
    let dlat = f64::from(be_u32(body, 67)) / ANGLE_SCALE;

    Ok(Grid::LatLon {
        nx,
        ny,
        la1,
        lo1: normalise_longitude(lo1),
        dlon,
        dlat,
        scan_mode: body[71],
    })
}

fn parse_lambert(body: &[u8]) -> Result<Grid, GribError> {
    if body.len() < LAMBERT_MIN_LEN {
        return Err(GribError::Truncated {
            what: "a template 3.30 grid definition",
            needed: LAMBERT_MIN_LEN,
            available: body.len(),
        });
    }

    let nx = be_u32(body, 30) as usize;
    let ny = be_u32(body, 34) as usize;

    let la1 = f64::from(signed_u32(body, 38)) / ANGLE_SCALE;
    let lo1 = f64::from(be_u32(body, 42)) / ANGLE_SCALE;
    let lad = f64::from(signed_u32(body, 47)) / ANGLE_SCALE;
    let lov = f64::from(be_u32(body, 51)) / ANGLE_SCALE;

    let dx = f64::from(be_u32(body, 55)) / LENGTH_SCALE * 1000.0;
    let dy = f64::from(be_u32(body, 59)) / LENGTH_SCALE * 1000.0;

    let latin1 = f64::from(signed_u32(body, 65)) / ANGLE_SCALE;
    let latin2 = f64::from(signed_u32(body, 69)) / ANGLE_SCALE;

    Ok(Grid::Lambert {
        nx,
        ny,
        la1,
        lo1: normalise_longitude(lo1),
        lad,
        lov: normalise_longitude(lov),
        dx,
        dy,
        latin1,
        latin2,
        scan_mode: body[64],
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn longitudes_are_normalised_into_pm180() {
        // HRRR's reference meridian arrives as 262.5 degrees east.
        assert!((normalise_longitude(262.5) - (-97.5)).abs() < 1e-9);

        // A longitude already in range is left alone.
        assert!((normalise_longitude(-97.5) - (-97.5)).abs() < 1e-9);
        assert!((normalise_longitude(12.0) - 12.0).abs() < 1e-9);
    }

    #[test]
    fn an_unknown_template_is_named() {
        let mut body = vec![0u8; 40];
        body[TEMPLATE_NUMBER_AT] = 0;
        body[TEMPLATE_NUMBER_AT + 1] = 40;

        assert_eq!(parse(&body), Err(GribError::UnsupportedGridTemplate(40)));
    }
}
