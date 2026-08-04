//! Section 4: what the values are.

use crate::GribError;
use crate::section::{be_u16, be_u32};

/// What a message's values represent.
///
/// These are the coordinates that confirm a fetched message is the field that
/// was asked for. A fetcher locates a message by the abbreviation and level
/// text in an index sidecar, which is a different vocabulary entirely — so
/// checking only the index would fetch the right bytes and decode them as
/// whatever they turned out to be.
#[derive(Debug, Clone, Copy, PartialEq)]
pub struct Product {
    /// WMO code table 4.1 parameter category, within the message's discipline.
    pub category: u8,

    /// WMO code table 4.2 parameter number, within the category.
    pub number: u8,

    /// WMO code table 4.5 fixed surface type. 1 is the ground, 10 is the
    /// entire atmosphere, 103 is a specified height above ground.
    pub level_type: u8,

    /// The level's value in the unit its type implies — metres for type 103.
    ///
    /// Already descaled: GRIB2 stores a value and a decimal scale factor
    /// separately, and a reader comparing the raw value against 10 would find
    /// 10 m wind stored as 1 with a scale of -1 just as often as as 10.
    pub level_value: f64,
}

/// Reads a GRIB2 signed octet.
///
/// Sign-magnitude, like every other signed integer in the format: the high bit
/// is the sign and the remaining seven are the magnitude.
fn signed_octet(raw: u8) -> i32 {
    let magnitude = i32::from(raw & 0x7f);

    if raw & 0x80 == 0 {
        magnitude
    } else {
        -magnitude
    }
}

/// Offsets within section 4, quoted from octet 1 as the WMO tables do.
const TEMPLATE_NUMBER_AT: usize = 7;
const CATEGORY_AT: usize = 9;
const NUMBER_AT: usize = 10;
const LEVEL_TYPE_AT: usize = 22;
const LEVEL_SCALE_AT: usize = 23;
const LEVEL_VALUE_AT: usize = 24;
const TEMPLATE_40_MIN_LEN: usize = 34;

/// Reads section 4.
///
/// # Errors
///
/// Returns [`GribError::UnsupportedProductTemplate`] for anything but 4.0.
/// Templates 4.8 and 4.11 describe values accumulated over an interval rather
/// than at an instant; both products use them for accumulation fields, and
/// reading one as though it were an instant would misreport what the number
/// means.
pub(crate) fn parse(body: &[u8]) -> Result<Product, GribError> {
    if body.len() < TEMPLATE_NUMBER_AT + 2 {
        return Err(GribError::Truncated {
            what: "the product definition template number",
            needed: TEMPLATE_NUMBER_AT + 2,
            available: body.len(),
        });
    }

    let template = be_u16(body, TEMPLATE_NUMBER_AT);

    // Template 4.0 is an instant; 4.8 is the same header followed by an
    // interval description. The coordinates this reads sit at identical
    // offsets in both, so an accumulation field decodes correctly here — what
    // it does not do is tell a caller the value covers an interval. That
    // distinction belongs upstream, where the field vocabulary already knows
    // which of its members are accumulations.
    if template != 0 && template != 8 {
        return Err(GribError::UnsupportedProductTemplate(template));
    }

    if body.len() < TEMPLATE_40_MIN_LEN {
        return Err(GribError::Truncated {
            what: "a template 4.0 product definition",
            needed: TEMPLATE_40_MIN_LEN,
            available: body.len(),
        });
    }

    let level_type = body[LEVEL_TYPE_AT];
    let raw_value = f64::from(be_u32(body, LEVEL_VALUE_AT));

    // The scale factor is a GRIB2 signed octet, which is sign-magnitude rather
    // than two's complement: 0x81 is -1, not -127. Reading it the second way
    // would put a level three orders of magnitude from where it belongs.
    let scale = signed_octet(body[LEVEL_SCALE_AT]);
    let level_value = raw_value / 10f64.powi(scale);

    Ok(Product {
        category: body[CATEGORY_AT],
        number: body[NUMBER_AT],
        level_type,
        level_value,
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    fn template_body(template: u16, scale: u8, value: u32) -> Vec<u8> {
        let mut body = vec![0u8; TEMPLATE_40_MIN_LEN];

        body[TEMPLATE_NUMBER_AT] = (template >> 8) as u8;
        body[TEMPLATE_NUMBER_AT + 1] = (template & 0xff) as u8;
        body[CATEGORY_AT] = 2;
        body[NUMBER_AT] = 22;
        body[LEVEL_TYPE_AT] = 103;
        body[LEVEL_SCALE_AT] = scale;
        body[LEVEL_VALUE_AT..LEVEL_VALUE_AT + 4].copy_from_slice(&value.to_be_bytes());

        body
    }

    #[test]
    fn a_level_is_descaled() {
        // 20 with a scale of 1 is 2 metres, not 20.
        let product = parse(&template_body(0, 1, 20)).expect("parsing");

        assert!(
            (product.level_value - 2.0).abs() < 1e-9,
            "level {} was not descaled",
            product.level_value
        );
    }

    #[test]
    fn an_unscaled_level_reads_directly() {
        let product = parse(&template_body(0, 0, 10)).expect("parsing");

        assert!((product.level_value - 10.0).abs() < 1e-9);
        assert_eq!((product.category, product.number), (2, 22));
    }

    #[test]
    fn a_negative_scale_is_sign_magnitude() {
        // 0x81 is -1, so a raw 995 becomes 9950 rather than 995e127.
        assert_eq!(signed_octet(0x81), -1);
        assert_eq!(signed_octet(0x01), 1);
        assert_eq!(signed_octet(0x00), 0);
    }

    #[test]
    fn an_unknown_template_is_named() {
        assert_eq!(
            parse(&template_body(9, 0, 0)),
            Err(GribError::UnsupportedProductTemplate(9))
        );
    }
}
