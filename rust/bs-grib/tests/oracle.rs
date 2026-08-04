//! Comparison against ecCodes, ECMWF's own GRIB implementation.
//!
//! The project's definition of done asks for tolerance tests against an
//! established reference implementation, and this is that test. Everything
//! else in this crate's suite checks the decoder against itself or against a
//! plausibility band: a decode wrong by a constant factor, or transposed, or
//! shifted by one row, would pass every one of those and fail here.
//!
//! ecCodes runs out of process — the golden files are produced by
//! `golden/generate.sh` and committed — so nothing in the build or the test
//! path links against it. That is the same posture the project takes toward
//! every external oracle.
//!
//! The golden is a strided sample rather than a whole field. 1.9 million
//! values per message is not something to commit, and a stride coprime with
//! the row width visits every part of the grid rather than walking one column.

use std::path::PathBuf;

/// One reference point: where ecCodes says it is, and what it says is there.
struct Reference {
    index: usize,
    lat: f64,
    lon: f64,
    value: f64,
}

/// The whole-field statistics ecCodes reports.
struct Stats {
    count: usize,
    min: f64,
    max: f64,
    avg: f64,
}

fn fixture(name: &str) -> Vec<u8> {
    let path = PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join("tests")
        .join("fixtures")
        .join(name);

    std::fs::read(&path).unwrap_or_else(|e| panic!("reading {}: {e}", path.display()))
}

fn golden(name: &str) -> (Stats, Vec<Reference>) {
    let path = PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join("tests")
        .join("golden")
        .join(name);

    let text = std::fs::read_to_string(&path).unwrap_or_else(|e| {
        panic!(
            "reading {}: {e}; run tests/golden/generate.sh",
            path.display()
        )
    });

    let mut stats = None;
    let mut points = Vec::new();

    for line in text.lines() {
        if let Some(rest) = line.strip_prefix("#stats\t") {
            let field: Vec<&str> = rest.split('\t').filter(|f| !f.is_empty()).collect();

            stats = Some(Stats {
                count: field[0].parse().expect("count"),
                min: field[1].parse().expect("min"),
                max: field[2].parse().expect("max"),
                avg: field[3].parse().expect("avg"),
            });

            continue;
        }

        if line.starts_with('#') || line.trim().is_empty() {
            continue;
        }

        let field: Vec<&str> = line.split('\t').collect();

        points.push(Reference {
            index: field[0].parse().expect("index"),
            lat: field[1].parse().expect("lat"),
            lon: field[2].parse().expect("lon"),
            value: field[3].parse().expect("value"),
        });
    }

    (stats.expect("a #stats line"), points)
}

/// ecCodes reports longitudes in 0..360; this crate normalises to ±180.
fn to_pm180(degrees: f64) -> f64 {
    if degrees > 180.0 {
        degrees - 360.0
    } else {
        degrees
    }
}

/// Compares one fixture's decoded values against the reference, point by
/// point and in aggregate.
fn assert_matches_oracle(fixture_name: &str, golden_name: &str) {
    let message = bs_grib::parse(&fixture(fixture_name)).expect("parsing");
    let (stats, points) = golden(golden_name);

    assert_eq!(
        message.values.len(),
        stats.count,
        "{fixture_name}: value count disagrees with ecCodes"
    );

    // Aggregate first: a decode wrong by a constant factor shows up here even
    // if every sampled point happened to be zero.
    let min = message.values.iter().copied().fold(f64::INFINITY, f64::min);
    let max = message
        .values
        .iter()
        .copied()
        .fold(f64::NEG_INFINITY, f64::max);
    // Counts and grid indices are in the millions at most, far inside f64's
    // exact-integer range — the type permits a loss the values cannot reach.
    #[allow(clippy::cast_precision_loss)]
    let avg = message.values.iter().sum::<f64>() / (message.values.len() as f64);

    assert!(
        (min - stats.min).abs() < 1e-6,
        "{fixture_name}: minimum {min} against ecCodes {}",
        stats.min
    );
    assert!(
        (max - stats.max).abs() < 1e-6,
        "{fixture_name}: maximum {max} against ecCodes {}",
        stats.max
    );

    // The average is printed to six significant figures, so it is compared
    // relatively rather than absolutely.
    let tolerance = stats.avg.abs().mul_add(1e-5, 1e-9);

    assert!(
        (avg - stats.avg).abs() < tolerance,
        "{fixture_name}: mean {avg} against ecCodes {}",
        stats.avg
    );

    // Then point by point. This is what catches a field that is transposed,
    // reversed, or shifted by a row — all of which preserve the aggregates
    // exactly.
    for point in &points {
        let decoded = message.values[point.index];

        assert!(
            (decoded - point.value).abs() < 1e-6,
            "{fixture_name}: index {} decoded {decoded}, ecCodes says {}",
            point.index,
            point.value
        );
    }
}

/// The geometry check: ecCodes states the position of every point it reports,
/// so the projection can be verified against the same reference as the values.
fn assert_geometry_matches_oracle(fixture_name: &str, golden_name: &str) {
    let message = bs_grib::parse(&fixture(fixture_name)).expect("parsing");
    let (_, points) = golden(golden_name);
    let (nx, _) = message.header.grid.shape();

    for point in &points {
        let located = message
            .header
            .grid
            .locate(to_pm180(point.lon), point.lat)
            .unwrap_or_else(|| {
                panic!(
                    "{fixture_name}: ecCodes places index {} at {}, {} — outside this \
                     grid's domain",
                    point.index, point.lat, point.lon
                )
            });

        #[allow(clippy::cast_precision_loss)]
        let (expected_i, expected_j) = ((point.index % nx) as f64, (point.index / nx) as f64);

        // A twentieth of a cell. The golden carries six decimals of
        // coordinate, so the remaining disagreement is this crate's spherical
        // projection against ecCodes' — and it is nowhere near the half-cell
        // error a sign or row-order mistake would produce.
        assert!(
            (located.0 - expected_i).abs() < 0.05 && (located.1 - expected_j).abs() < 0.05,
            "{fixture_name}: ecCodes puts index {} at grid ({expected_i}, {expected_j}); \
             this crate locates {}, {} at ({}, {})",
            point.index,
            point.lat,
            point.lon,
            located.0,
            located.1
        );
    }
}

/// The varied field: complex packing with spatial differencing, 10733 groups.
/// This is the one that matters.
#[test]
fn complex_packing_matches_eccodes() {
    assert_matches_oracle("hrrr_ltng_atmos.grib2", "hrrr_ltng_atmos.golden");
}

#[test]
fn constant_field_matches_eccodes() {
    assert_matches_oracle("hrrr_cicep_surface.grib2", "hrrr_cicep_surface.golden");
}

#[test]
fn gfs_field_matches_eccodes() {
    assert_matches_oracle("gfs_clmr_50mb.grib2", "gfs_clmr_50mb.golden");
}

#[test]
fn lambert_projection_matches_eccodes() {
    assert_geometry_matches_oracle("hrrr_ltng_atmos.grib2", "hrrr_ltng_atmos.golden");
}

#[test]
fn latlon_projection_matches_eccodes() {
    assert_geometry_matches_oracle("gfs_clmr_50mb.grib2", "gfs_clmr_50mb.golden");
}
