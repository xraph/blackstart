//! Walking a GRIB2 message's sections.
//!
//! After the fixed 16-byte indicator, a message is a run of length-prefixed
//! sections: four bytes of length, one byte of section number, then the body.
//! The run ends at the literal `7777`. Every multi-byte integer in GRIB2 is
//! big-endian, without exception.

use crate::GribError;

/// One section, with its body including the length and number prefix.
///
/// The body keeps its prefix because every offset in the WMO tables is quoted
/// from the start of the section, octet 1. Trimming the prefix would mean
/// subtracting five from every documented offset in the parsers, which is a
/// transcription error waiting to happen.
#[derive(Debug, Clone, Copy)]
pub(crate) struct Section<'a> {
    /// The section number, 1 through 7.
    pub(crate) number: u8,
    /// The section including its length and number prefix.
    pub(crate) body: &'a [u8],
}

/// The section header is four length bytes plus one number byte.
const HEADER_LEN: usize = 5;

/// Reads a big-endian u64.
pub(crate) fn be_u64(bytes: &[u8], at: usize) -> u64 {
    let mut out = 0u64;

    for byte in &bytes[at..at + 8] {
        out = (out << 8) | u64::from(*byte);
    }

    out
}

/// Reads a big-endian u32.
pub(crate) fn be_u32(bytes: &[u8], at: usize) -> u32 {
    let mut out = 0u32;

    for byte in &bytes[at..at + 4] {
        out = (out << 8) | u32::from(*byte);
    }

    out
}

/// Reads a big-endian u16.
pub(crate) fn be_u16(bytes: &[u8], at: usize) -> u16 {
    (u16::from(bytes[at]) << 8) | u16::from(bytes[at + 1])
}

/// Reads a GRIB2 signed integer.
///
/// Not two's complement. GRIB2 signs a value with the high bit of its first
/// octet and stores the magnitude in the rest, so a naive cast reads -1 as a
/// very large positive number — and a latitude read that way lands the grid in
/// the wrong hemisphere.
pub(crate) fn signed_u32(bytes: &[u8], at: usize) -> i32 {
    let raw = be_u32(bytes, at);

    // The magnitude is 31 bits, so it always fits an i32 — and returning i32
    // rather than a wider type is what lets callers reach f64 through
    // `f64::from`, which is infallible and lossless, instead of an `as` cast
    // the numerical lints rightly refuse.
    let magnitude = i32::try_from(raw & 0x7fff_ffff).unwrap_or(i32::MAX);

    if raw & 0x8000_0000 != 0 {
        -magnitude
    } else {
        magnitude
    }
}

/// Walks a message's sections, stopping at the terminator.
///
/// # Errors
///
/// Returns [`GribError::Truncated`] when a section claims a length that runs
/// past the end of the message, which is what a half-downloaded byte range
/// looks like.
pub(crate) fn walk(bytes: &[u8]) -> Result<Vec<Section<'_>>, GribError> {
    let mut sections = Vec::with_capacity(8);
    let mut at = crate::INDICATOR_LEN;

    while at + 4 <= bytes.len() {
        if &bytes[at..at + 4] == b"7777" {
            return Ok(sections);
        }

        if at + HEADER_LEN > bytes.len() {
            return Err(GribError::Truncated {
                what: "a section header",
                needed: at + HEADER_LEN,
                available: bytes.len(),
            });
        }

        let length = be_u32(bytes, at) as usize;

        // A zero-length section would loop forever. It cannot occur in a
        // well-formed message, which is exactly why it must be refused rather
        // than trusted.
        if length < HEADER_LEN || at + length > bytes.len() {
            return Err(GribError::Truncated {
                what: "a section body",
                needed: at + length.max(HEADER_LEN),
                available: bytes.len(),
            });
        }

        let number = bytes[at + 4];

        sections.push(Section {
            number,
            body: &bytes[at..at + length],
        });

        at += length;
    }

    // Running out of bytes before the terminator is the signature of a
    // truncated download, so it is an error rather than a quiet stop.
    Err(GribError::Truncated {
        what: "the section terminator",
        needed: at + 4,
        available: bytes.len(),
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn signed_values_use_sign_magnitude() {
        // 0x80000005 is -5 in GRIB2's convention and 2147483653 as two's
        // complement. Reading a latitude the second way puts the grid in the
        // southern hemisphere.
        assert_eq!(signed_u32(&[0x80, 0x00, 0x00, 0x05], 0), -5);
        assert_eq!(signed_u32(&[0x00, 0x00, 0x00, 0x05], 0), 5);

        // The widest magnitude still fits, which is the property that lets
        // this return i32 at all.
        assert_eq!(signed_u32(&[0x7f, 0xff, 0xff, 0xff], 0), i32::MAX);
    }

    #[test]
    fn big_endian_readers_agree_with_their_widths() {
        let bytes = [0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08];

        assert_eq!(be_u16(&bytes, 0), 0x0102);
        assert_eq!(be_u32(&bytes, 0), 0x0102_0304);
        assert_eq!(be_u64(&bytes, 0), 0x0102_0304_0506_0708);
    }

    #[test]
    fn a_message_with_no_terminator_is_truncated() {
        let mut bytes = vec![0u8; crate::INDICATOR_LEN];
        bytes.extend_from_slice(&[0, 0, 0, 6, 1, 0]);

        assert!(matches!(walk(&bytes), Err(GribError::Truncated { .. })));
    }
}
