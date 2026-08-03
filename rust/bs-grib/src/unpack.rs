//! Section 5 and section 7: how the values are packed, and unpacking them.
//!
//! Two templates, both of which the products use.
//!
//! Template 5.0 is simple packing: every value is the same width, and
//! `value = (R + x * 2^E) / 10^D`. With zero bits per value the data section
//! is empty and every point equals `R / 10^D` — which is what a categorical
//! field looks like when its phase occurs nowhere in the domain, and is
//! therefore the common case rather than a curiosity.
//!
//! Template 5.3, complex packing with spatial differencing, lands next. It is
//! where almost all of a real message's bytes go.

use crate::GribError;
use crate::section::{be_f32, be_u16, signed_u16};

/// How a message's values are packed.
#[derive(Debug, Clone, PartialEq)]
pub(crate) enum Packing {
    /// Template 5.0.
    Simple(Scaling),
}

/// The reference, binary scale and decimal scale that turn an integer back
/// into a physical value.
#[derive(Debug, Clone, Copy, PartialEq)]
pub(crate) struct Scaling {
    /// The reference value every packed integer is expressed above.
    pub(crate) reference: f32,
    /// The binary scale exponent.
    pub(crate) binary: i32,
    /// The decimal scale exponent.
    pub(crate) decimal: i32,
    /// Bits per packed value.
    pub(crate) bits: u8,
}

impl Scaling {
    /// Turns a packed integer into its physical value.
    ///
    /// `value = (R + x * 2^E) / 10^D`, which is the formula GRIB2 states. The
    /// exponent is applied with `powi` rather than a bit shift because E is
    /// routinely negative, and often very negative — a real HRRR gust field
    /// carries E = -32764.
    fn apply(self, packed: i64) -> f64 {
        // The cast is exact in fact though not in type. A packed value is at
        // most `bits` wide and `bits` is a single octet, so it cannot exceed
        // 2^255 in theory and never exceeds 2^32 in these products — either
        // way far inside f64's 53-bit exact-integer range. Clippy cannot see
        // that bound, and widening the arithmetic to a bigger type would not
        // help since the destination is f64 regardless.
        #[allow(clippy::cast_precision_loss)]
        let magnitude = packed as f64;

        let scaled = f64::from(self.reference) + magnitude * 2f64.powi(self.binary);

        scaled / 10f64.powi(self.decimal)
    }
}

// Offsets within section 5, as index N-1 for the WMO tables' octet N.
const TEMPLATE_NUMBER_AT: usize = 9;
const REFERENCE_AT: usize = 11;
const BINARY_SCALE_AT: usize = 15;
const DECIMAL_SCALE_AT: usize = 17;
const BITS_AT: usize = 19;
const SIMPLE_MIN_LEN: usize = 21;

/// Reads section 5.
///
/// # Errors
///
/// Returns [`GribError::UnsupportedDataTemplate`] for anything but 5.0 and
/// 5.3.
pub(crate) fn parse(body: &[u8]) -> Result<Packing, GribError> {
    if body.len() < SIMPLE_MIN_LEN {
        return Err(GribError::Truncated {
            what: "a data representation section",
            needed: SIMPLE_MIN_LEN,
            available: body.len(),
        });
    }

    let scaling = Scaling {
        reference: be_f32(body, REFERENCE_AT),
        binary: signed_u16(body, BINARY_SCALE_AT),
        decimal: signed_u16(body, DECIMAL_SCALE_AT),
        bits: body[BITS_AT],
    };

    match be_u16(body, TEMPLATE_NUMBER_AT) {
        0 => Ok(Packing::Simple(scaling)),
        other => Err(GribError::UnsupportedDataTemplate(other)),
    }
}

/// A big-endian bit stream.
///
/// GRIB2 packs values at arbitrary widths with no byte alignment between them,
/// so a 12-bit field straddles byte boundaries two times in three. Reading one
/// byte at a time and shifting is the whole of it.
struct BitReader<'a> {
    bytes: &'a [u8],
    bit: usize,
}

impl<'a> BitReader<'a> {
    fn new(bytes: &'a [u8]) -> Self {
        Self { bytes, bit: 0 }
    }

    /// Reads `width` bits as an unsigned integer.
    ///
    /// A width of zero yields zero without consuming anything, which is not a
    /// special case bolted on: a group whose values are all equal is encoded
    /// with a width of zero, and it is the most common group in a field with
    /// large constant regions.
    fn read(&mut self, width: u8) -> u64 {
        let mut out = 0u64;

        for _ in 0..width {
            let byte = self.bit / 8;
            let offset = self.bit % 8;

            let value = if byte < self.bytes.len() {
                u64::from((self.bytes[byte] >> (7 - offset)) & 1)
            } else {
                // Past the end reads as zero rather than panicking. The
                // caller checks the decoded count against the grid size, which
                // catches a short section as a mismatch naming both numbers.
                0
            };

            out = (out << 1) | value;
            self.bit += 1;
        }

        out
    }
}

/// Unpacks a data section.
///
/// `expected` is the grid's point count. It is a parameter rather than
/// something derived from the data because a constant field's data section is
/// empty: the count is knowable only from the grid, and returning a short
/// field for the caller to pad would put the grid's dimensions in two places.
///
/// Infallible: every template this accepts was already validated when section
/// 5 was parsed, so there is nothing left here to reject. A short or corrupt
/// data section produces too few values, which the caller catches by comparing
/// the count against the grid.
pub(crate) fn decode(packing: &Packing, data: &[u8], expected: usize) -> Vec<f64> {
    match packing {
        Packing::Simple(scaling) => decode_simple(*scaling, data, expected),
    }
}

/// Template 5.0.
///
/// With zero bits per value the whole field is the reference value, and the
/// count comes from the grid — the data section is empty, so there is nothing
/// here to count.
fn decode_simple(scaling: Scaling, data: &[u8], expected: usize) -> Vec<f64> {
    if scaling.bits == 0 {
        // Every point equals the reference. This is what a categorical field
        // looks like when its phase occurs nowhere in the domain, which is
        // most hours of most days.
        return vec![scaling.apply(0); expected];
    }

    let total = (data.len() * 8) / usize::from(scaling.bits);

    let mut reader = BitReader::new(data);
    let mut out = Vec::with_capacity(total);

    for _ in 0..total {
        let packed = reader.read(scaling.bits);
        out.push(scaling.apply(i64::try_from(packed).unwrap_or(i64::MAX)));
    }

    out
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_zero_width_read_consumes_nothing() {
        let mut reader = BitReader::new(&[0xff, 0xff]);

        assert_eq!(reader.read(0), 0);
        assert_eq!(reader.bit, 0, "a zero-width read moved the cursor");
        assert_eq!(reader.read(8), 0xff);
    }

    #[test]
    fn reads_straddle_byte_boundaries() {
        // 0b1010_1100 0b1100_0000: two 5-bit reads are 10101 and 10011.
        let mut reader = BitReader::new(&[0b1010_1100, 0b1100_0000]);

        assert_eq!(reader.read(5), 0b10101);
        assert_eq!(reader.read(5), 0b10011);
    }

    #[test]
    fn reading_past_the_end_yields_zero() {
        let mut reader = BitReader::new(&[0xff]);

        assert_eq!(reader.read(8), 0xff);
        assert_eq!(reader.read(8), 0, "past the end must not panic");
    }

    #[test]
    fn scaling_applies_the_grib_formula() {
        let scaling = Scaling {
            reference: 1.0,
            binary: 1,
            decimal: 1,
            bits: 8,
        };

        // (1 + 4 * 2^1) / 10^1 = 9 / 10.
        assert!((scaling.apply(4) - 0.9).abs() < 1e-12);
    }
}
