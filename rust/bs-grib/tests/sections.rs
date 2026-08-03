//! Section walking against real NOAA messages.

use std::path::PathBuf;

fn fixture(name: &str) -> Vec<u8> {
    let path = PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join("tests")
        .join("fixtures")
        .join(name);

    std::fs::read(&path).unwrap_or_else(|e| panic!("reading {}: {e}", path.display()))
}

/// A well-formed message carries sections 1 through 7, in order.
#[test]
fn a_real_message_has_the_expected_sections() {
    let sections = bs_grib::section_lengths(&fixture("hrrr_ltng_atmos.grib2")).expect("walking");

    let numbers: Vec<u8> = sections.iter().map(|(n, _)| *n).collect();

    assert_eq!(numbers, vec![1, 3, 4, 5, 6, 7], "section numbers in order");
}

/// A constant field's section 7 holds nothing but its own header — which is
/// what makes it the case a decoder has to handle rather than assume away.
#[test]
fn a_constant_field_has_an_empty_data_section() {
    let sections = bs_grib::section_lengths(&fixture("hrrr_cicep_surface.grib2")).expect("walking");

    let data = sections.iter().find(|(n, _)| *n == 7).expect("a section 7");

    assert_eq!(data.1, 5, "section 7 should be its header alone");
}

#[test]
fn garbage_is_refused() {
    assert!(bs_grib::section_lengths(b"nowhere near a grib message").is_err());
}
