//! Decoder tests against real NOAA messages.
//!
//! Every fixture here was byte-ranged out of a published product. A GRIB2
//! decoder cannot be meaningfully tested against synthetic input: the formats
//! that matter are the ones NOAA actually emits, and the bugs that matter are
//! the ones where a plausible number comes out of a misread stream.

use std::path::PathBuf;

use bs_grib::{GribError, Grid};

fn fixture(name: &str) -> Vec<u8> {
    let path = PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join("tests")
        .join("fixtures")
        .join(name);

    std::fs::read(&path).unwrap_or_else(|e| panic!("reading {}: {e}", path.display()))
}

/// HRRR's grid, read from a real message rather than from documentation.
#[test]
fn hrrr_is_lambert_conformal() {
    let message = bs_grib::parse(&fixture("hrrr_cicep_surface.grib2")).expect("parsing HRRR");

    let Grid::Lambert {
        nx,
        ny,
        latin1,
        latin2,
        lad,
        lov,
        dx,
        dy,
        ..
    } = message.header.grid
    else {
        panic!("HRRR decoded as {:?}, want Lambert", message.header.grid);
    };

    assert_eq!((nx, ny), (1799, 1059), "HRRR CONUS grid dimensions");

    // Latin1 == Latin2 means the cone is tangent rather than secant, which is
    // what lets the cone constant be sin(latin1). The sampler relies on
    // handling both, so this pins which one HRRR actually is.
    assert!(
        (latin1 - 38.5).abs() < 1e-6 && (latin2 - 38.5).abs() < 1e-6,
        "standard parallels: {latin1} / {latin2}"
    );

    assert!((lad - 38.5).abs() < 1e-6, "LaD: {lad}");
    assert!(
        (lov - (-97.5)).abs() < 1e-6,
        "LoV normalised to ±180: {lov}"
    );

    // 3 km, expressed in metres.
    assert!(
        (dx - 3000.0).abs() < 1.0 && (dy - 3000.0).abs() < 1.0,
        "{dx} x {dy}"
    );
}

/// GFS's grid is the other template the products use.
#[test]
fn gfs_is_regular_latlon() {
    let message = bs_grib::parse(&fixture("gfs_clmr_50mb.grib2")).expect("parsing GFS");

    let Grid::LatLon {
        nx, ny, dlon, dlat, ..
    } = message.header.grid
    else {
        panic!("GFS decoded as {:?}, want LatLon", message.header.grid);
    };

    assert_eq!((nx, ny), (1440, 721), "GFS 0.25 degree global grid");
    assert!((dlon - 0.25).abs() < 1e-6 && (dlat - 0.25).abs() < 1e-6);
}

/// The product section is what confirms a fetched message is the field that
/// was asked for. Matching only the index abbreviation would fetch the right
/// bytes and decode them as the wrong field.
#[test]
fn product_coordinates_are_decoded() {
    let message = bs_grib::parse(&fixture("hrrr_cicep_surface.grib2")).expect("parsing HRRR");

    assert_eq!(message.header.discipline, 0, "meteorological products");

    // Categorical ice pellets carries the WMO standard number 35, not the
    // NCEP local-table 194 its CICEP abbreviation suggests.
    assert_eq!(message.header.product.category, 1);
    assert_eq!(message.header.product.number, 35);
    assert_eq!(message.header.product.level_type, 1, "surface");
}

/// Template 5.0 with zero bits per value: every point equals the reference,
/// and section 7 is empty. The categorical fields are like this whenever the
/// phase does not occur anywhere in the domain, which is most of the time.
///
/// It is the degenerate case an untested decoder gets wrong first, because
/// nothing about it looks like decoding.
#[test]
fn constant_field_has_no_data_section() {
    let message = bs_grib::parse(&fixture("hrrr_cicep_surface.grib2")).expect("parsing CICEP");

    assert_eq!(
        message.values.len(),
        1799 * 1059,
        "a constant field still covers the whole grid"
    );

    assert!(
        message.values.iter().all(|v| *v == 0.0),
        "every value should equal the reference"
    );
}

/// The lightning field is a real varied one: 12 bits per value over 10733
/// groups, with first-order spatial differencing. This is the path that
/// actually exercises complex packing.
#[test]
fn varied_field_decodes_to_plausible_values() {
    let message = bs_grib::parse(&fixture("hrrr_ltng_atmos.grib2")).expect("parsing HRRR");

    assert_eq!(message.values.len(), 1799 * 1059);

    assert!(
        message.values.iter().all(|v| v.is_finite()),
        "a NaN or infinity means the bit stream was misread"
    );

    let min = message.values.iter().copied().fold(f64::INFINITY, f64::min);
    let max = message
        .values
        .iter()
        .copied()
        .fold(f64::NEG_INFINITY, f64::max);

    // A lightning threat index is non-negative and does not reach far into
    // double digits. This is a plausibility band, not a reference comparison —
    // see the note in SOURCES.md about the missing oracle.
    assert!(min >= 0.0, "minimum {min} is negative");
    assert!(max > 0.0, "maximum {max} means nothing was decoded");
    assert!(
        max < 1000.0,
        "maximum {max} is not a lightning threat index"
    );
}

/// An unsupported template is an error naming the number, never a best-effort
/// parse. A misread packing produces plausible wrong numbers, which is the
/// failure mode this project's numerical lints exist to prevent everywhere.
#[test]
fn truncated_input_is_refused() {
    let full = fixture("hrrr_cicep_surface.grib2");

    match bs_grib::parse(&full[..full.len() / 2]) {
        Err(GribError::Truncated { .. }) => {}
        other => panic!("truncated input gave {other:?}, want Truncated"),
    }
}

#[test]
fn non_grib_input_is_refused() {
    match bs_grib::parse(b"not a grib file at all, not even close") {
        Err(GribError::NotGrib) => {}
        other => panic!("garbage gave {other:?}, want NotGrib"),
    }
}

/// The whole point, end to end: a real message decoded and sampled at a real
/// place.
///
/// Austin sits well inside CONUS, so a lightning threat index there is a
/// finite non-negative number. What this really pins is that decoding and
/// projection agree — a field decoded correctly but indexed with a flipped
/// row order would still be finite and still be in range.
#[test]
fn a_real_field_samples_inside_conus() {
    let message = bs_grib::parse(&fixture("hrrr_ltng_atmos.grib2")).expect("parsing HRRR");

    let value = message
        .sample(-97.67, 30.19)
        .expect("Austin is inside the HRRR domain");

    assert!(value.is_finite(), "sampled {value}");
    assert!(value >= 0.0, "a lightning threat index cannot be negative");
}

/// A span outside the domain has no sample, not a zero. A fabricated calm is
/// the failure this returns None to prevent.
#[test]
fn a_point_outside_conus_has_no_sample() {
    let message = bs_grib::parse(&fixture("hrrr_ltng_atmos.grib2")).expect("parsing HRRR");

    assert_eq!(
        message.sample(0.0, 0.0),
        None,
        "the Gulf of Guinea is not in the HRRR domain"
    );
}

/// Sampling at the first grid point returns that point's own value, which is
/// the check that the interpolation weights are not shifted by half a cell.
#[test]
fn sampling_the_origin_returns_the_first_value() {
    let message = bs_grib::parse(&fixture("hrrr_ltng_atmos.grib2")).expect("parsing HRRR");

    let bs_grib::Grid::Lambert { la1, lo1, .. } = message.header.grid else {
        panic!("expected Lambert");
    };

    let sampled = message.sample(lo1, la1).expect("the origin is in domain");

    assert!(
        (sampled - message.values[0]).abs() < 1e-9,
        "sampling the origin gave {sampled}, the stored first value is {}",
        message.values[0]
    );
}
