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
    fn big_endian_readers_agree_with_their_widths() {
        let bytes = [0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08];

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
