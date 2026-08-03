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
//! Template 5.3 is complex packing with spatial differencing. The field is cut
//! into groups, each with its own reference and its own bit width, and the
//! whole sequence has been differenced first so that neighbouring values —
//! which are strongly correlated on a weather field — pack into few bits. It
//! is where almost all of a real message's bytes go and where a decoder gets
//! things wrong invisibly.

use crate::GribError;
use crate::section::{be_f32, be_u16, be_u32, signed_u16};

/// How a message's values are packed.
#[derive(Debug, Clone, PartialEq)]
pub(crate) enum Packing {
    /// Template 5.0.
    Simple(Scaling),

    /// Template 5.3.
    Complex {
        scaling: Scaling,
        /// How many values the field holds.
        count: usize,
        /// How many groups they are cut into.
        groups: usize,
        /// Bits per group reference value.
        reference_bits: u8,
        /// Bits per group width, above `width_reference`.
        width_bits: u8,
        /// The value every group width is expressed above.
        width_reference: u8,
        /// Bits per group length, above `length_reference`.
        length_bits: u8,
        /// The value every group length is expressed above.
        length_reference: u32,
        /// The multiplier applied to each group length.
        length_increment: u8,
        /// The last group's length, stated rather than encoded.
        last_group_length: u32,
        /// Order of spatial differencing: 1 or 2.
        spatial_order: u8,
        /// Octets used by each spatial-differencing seed.
        extra_octets: u8,
    },
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

// Offsets within section 5.
//
// The WMO tables number octets from 1, so octet N is index N-1 here. That
// conversion is the single most reliable way to misread this section: a first
// attempt put the group-length width at index 42 instead of 46 and the last
// group's length at 43 instead of 42, and decoded 74743 values for a grid of
// 1905141. It failed loudly only because the count is checked against the
// grid; a subtler slip would have produced a full field of wrong numbers.
//
//   octet 32-35  number of groups            index 31
//   octet 36     group width reference       index 35
//   octet 37     group width bits            index 36
//   octet 38-41  group length reference      index 37
//   octet 42     group length increment      index 41
//   octet 43-46  true length of last group   index 42
//   octet 47     group length bits           index 46
//   octet 48     spatial differencing order  index 47
//   octet 49     extra octets for the seeds  index 48
const COUNT_AT: usize = 5;
const TEMPLATE_NUMBER_AT: usize = 9;
const REFERENCE_AT: usize = 11;
const BINARY_SCALE_AT: usize = 15;
const DECIMAL_SCALE_AT: usize = 17;
const BITS_AT: usize = 19;
const SIMPLE_MIN_LEN: usize = 21;
const COMPLEX_MIN_LEN: usize = 49;

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
        3 => {
            if body.len() < COMPLEX_MIN_LEN {
                return Err(GribError::Truncated {
                    what: "a template 5.3 data representation section",
                    needed: COMPLEX_MIN_LEN,
                    available: body.len(),
                });
            }

            Ok(Packing::Complex {
                scaling,
                count: be_u32(body, COUNT_AT) as usize,
                groups: be_u32(body, 31) as usize,
                reference_bits: scaling.bits,
                width_bits: body[36],
                width_reference: body[35],
                length_bits: body[46],
                length_reference: be_u32(body, 37),
                length_increment: body[41],
                last_group_length: be_u32(body, 42),
                spatial_order: body[47],
                extra_octets: body[48],
            })
        }
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

    /// Skips to the next whole byte.
    fn align(&mut self) {
        self.bit = self.bit.div_ceil(8) * 8;
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
        Packing::Complex { .. } => decode_complex(packing, data),
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

/// Template 5.3.
#[allow(clippy::too_many_lines)]
fn decode_complex(packing: &Packing, data: &[u8]) -> Vec<f64> {
    let Packing::Complex {
        scaling,
        count,
        groups,
        reference_bits,
        width_bits,
        width_reference,
        length_bits,
        length_reference,
        length_increment,
        last_group_length,
        spatial_order,
        extra_octets,
    } = *packing
    else {
        unreachable!("decode_complex called with simple packing");
    };

    let mut reader = BitReader::new(data);

    // The spatial-differencing seeds come first: the initial value or values
    // the running sum starts from, and the minimum that was subtracted from
    // every difference to make them all non-negative.
    let seed_bits = extra_octets * 8;

    let mut seeds = Vec::with_capacity(2);

    for _ in 0..spatial_order {
        seeds.push(read_signed(&mut reader, seed_bits));
    }

    let minimum = read_signed(&mut reader, seed_bits);

    reader.align();

    // Group references, then widths, then lengths — each a full run before the
    // next begins, rather than interleaved per group.
    let mut references = Vec::with_capacity(groups);

    for _ in 0..groups {
        references.push(reader.read(reference_bits));
    }

    reader.align();

    let mut widths = Vec::with_capacity(groups);

    for _ in 0..groups {
        // A group width is a bit count, so it cannot exceed 64 in any
        // well-formed message. Saturating rather than wrapping keeps a corrupt
        // message from silently producing a narrow read.
        let above = u8::try_from(reader.read(width_bits)).unwrap_or(u8::MAX);

        widths.push(width_reference.saturating_add(above));
    }

    reader.align();

    let mut lengths = Vec::with_capacity(groups);

    for index in 0..groups {
        let raw = u32::try_from(reader.read(length_bits)).unwrap_or(u32::MAX);

        lengths.push(if index + 1 == groups {
            last_group_length
        } else {
            length_reference.saturating_add(raw.saturating_mul(u32::from(length_increment)))
        });
    }

    reader.align();

    // The values themselves, group by group, each at its own width above its
    // own reference.
    let mut packed: Vec<i64> = Vec::with_capacity(count);

    for index in 0..groups {
        let width = widths[index];
        let reference = i64::try_from(references[index]).unwrap_or(i64::MAX);

        for _ in 0..lengths[index] {
            let above = i64::try_from(reader.read(width)).unwrap_or(i64::MAX);

            packed.push(reference.saturating_add(above));
        }
    }

    undo_spatial_differencing(&mut packed, spatial_order, &seeds, minimum);

    packed.into_iter().map(|p| scaling.apply(p)).collect()
}

/// Reads a sign-magnitude integer of `bits` bits from the stream.
///
/// The spatial-differencing seeds are signed this way rather than in two's
/// complement, so a negative minimum read naively becomes an enormous positive
/// one and shifts the entire field.
fn read_signed(reader: &mut BitReader<'_>, bits: u8) -> i64 {
    if bits == 0 {
        return 0;
    }

    let raw = reader.read(bits);
    let sign_bit = 1u64 << (bits - 1);
    let magnitude = i64::try_from(raw & (sign_bit - 1)).unwrap_or(i64::MAX);

    if raw & sign_bit == 0 {
        i64::try_from(raw).unwrap_or(i64::MAX)
    } else {
        -magnitude
    }
}

/// Reverses the spatial differencing applied before packing.
///
/// First order stores each value's difference from its predecessor; second
/// order stores the difference of the differences. Both have had the sequence
/// minimum subtracted so every stored difference is non-negative, so that
/// minimum goes back on first.
fn undo_spatial_differencing(values: &mut [i64], order: u8, seeds: &[i64], minimum: i64) {
    if order == 0 || values.is_empty() {
        return;
    }

    let start = usize::from(order).min(values.len());

    for value in values.iter_mut().skip(start) {
        *value += minimum;
    }

    for (index, seed) in seeds.iter().enumerate().take(start) {
        values[index] = *seed;
    }

    match order {
        1 => {
            for index in start..values.len() {
                values[index] += values[index - 1];
            }
        }
        2 => {
            for index in start..values.len() {
                values[index] += 2 * values[index - 1] - values[index - 2];
            }
        }
        _ => {}
    }
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
    fn alignment_moves_to_the_next_byte() {
        let mut reader = BitReader::new(&[0xff, 0x0f]);

        reader.read(3);
        reader.align();

        assert_eq!(reader.bit, 8);
        assert_eq!(reader.read(8), 0x0f);
    }

    #[test]
    fn signed_seeds_use_sign_magnitude() {
        // 0b1000_0101 over 8 bits is -5, not 133.
        let mut reader = BitReader::new(&[0b1000_0101]);

        assert_eq!(read_signed(&mut reader, 8), -5);
    }

    #[test]
    fn first_order_differencing_is_a_running_sum() {
        // Seeded at 10, then differences 1, 2, 3 with a minimum of 0.
        let mut values = vec![0, 1, 2, 3];

        undo_spatial_differencing(&mut values, 1, &[10], 0);

        assert_eq!(values, vec![10, 11, 13, 16]);
    }

    #[test]
    fn the_minimum_goes_back_on_before_summing() {
        // Every difference had 2 subtracted from it before packing.
        let mut values = vec![0, 0, 0, 0];

        undo_spatial_differencing(&mut values, 1, &[5], 2);

        assert_eq!(values, vec![5, 7, 9, 11]);
    }

    #[test]
    fn second_order_differencing_uses_two_seeds() {
        let mut values = vec![0, 0, 0, 0];

        undo_spatial_differencing(&mut values, 2, &[1, 2], 0);

        // Constant second difference of zero is an arithmetic progression.
        assert_eq!(values, vec![1, 2, 3, 4]);
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
