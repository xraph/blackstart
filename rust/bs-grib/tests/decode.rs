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
