//! GRIB2 decoding for the weather fields this platform consumes.
//!
//! Deliberately narrow. GRIB2 is a container format with dozens of grid,
//! product and data-representation templates, and a general reader for it is a
//! large piece of work whose extra generality nothing here would exercise.
//! This decodes the templates NOAA's HRRR and GFS products actually use,
//! verified against real messages, and refuses everything else by name.
//!
//! Refusing by name is the point. A misread packing does not crash — it
//! produces a field of plausible wrong numbers, which would flow into a hazard
//! calculation and out to an operator with nothing anywhere to say it was
//! wrong. Every unsupported template is an error carrying its own number.
//!
//! # Supported
//!
//! | Section | Template | Used by |
//! |---|---|---|
//! | 3, grid | 3.0 regular lat/lon | GFS |
//! | 3, grid | 3.30 Lambert conformal | HRRR |
//! | 4, product | 4.0 analysis or forecast at a level | both |
//! | 5, data | 5.0 simple packing | both, for constant fields |
//! | 5, data | 5.3 complex packing, spatial differencing | both, for real fields |

mod grid;
mod product;
mod sample;
mod section;
mod unpack;

pub use grid::Grid;
pub use product::Product;

/// Everything that can go wrong reading a message.
///
/// Each variant names what was unsupported or malformed rather than
/// collapsing into one opaque failure, because the likely next step differs:
/// an unsupported template needs code, a truncated message needs a refetch.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum GribError {
    /// The bytes do not begin with the GRIB indicator.
    NotGrib,

    /// The edition is not 2. Edition 1 is a different format entirely.
    UnsupportedEdition(u8),

    /// The message claims a length the buffer does not contain, or a section
    /// runs past the end.
    Truncated {
        /// What was being read when the buffer ran out.
        what: &'static str,
        /// How many bytes were needed.
        needed: usize,
        /// How many were available.
        available: usize,
    },

    /// A grid definition template this decoder does not implement.
    UnsupportedGridTemplate(u16),

    /// A product definition template this decoder does not implement.
    UnsupportedProductTemplate(u16),

    /// A data representation template this decoder does not implement.
    UnsupportedDataTemplate(u16),

    /// A bitmap is present. None of the products this reads uses one, and
    /// decoding values without knowing which points they belong to would
    /// misplace every one of them.
    BitmapUnsupported(u8),

    /// A required section was absent.
    MissingSection(u8),

    /// The decoded value count disagrees with the grid size.
    ValueCountMismatch {
        /// How many values the data section produced.
        decoded: usize,
        /// How many the grid needs.
        expected: usize,
    },
}

impl std::fmt::Display for GribError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::NotGrib => write!(f, "not a GRIB message: no GRIB indicator"),
            Self::UnsupportedEdition(e) => {
                write!(f, "GRIB edition {e} is not supported; this reads edition 2")
            }
            Self::Truncated {
                what,
                needed,
                available,
            } => write!(
                f,
                "truncated while reading {what}: needed {needed} bytes, had {available}"
            ),
            Self::UnsupportedGridTemplate(t) => write!(
                f,
                "grid definition template 3.{t} is not supported; this reads 3.0 and 3.30"
            ),
            Self::UnsupportedProductTemplate(t) => write!(
                f,
                "product definition template 4.{t} is not supported; this reads 4.0"
            ),
            Self::UnsupportedDataTemplate(t) => write!(
                f,
                "data representation template 5.{t} is not supported; this reads 5.0 and 5.3"
            ),
            Self::BitmapUnsupported(i) => write!(
                f,
                "bitmap indicator {i}: this decoder has no bitmap support, and \
                 decoding values without one would misplace every point"
            ),
            Self::MissingSection(n) => write!(f, "section {n} is missing"),
            Self::ValueCountMismatch { decoded, expected } => {
                write!(f, "decoded {decoded} values for a grid of {expected}")
            }
        }
    }
}

impl std::error::Error for GribError {}

/// The fixed length of section 0.
const INDICATOR_LEN: usize = 16;

/// Lists the sections a message contains: each one's number and byte length.
///
/// Useful on its own rather than only as the walker's smoke test. An operator
/// asking why a message will not decode wants to know which sections are
/// present and how big each is before anything else — a section 7 of length
/// zero says "constant field", and a missing section 3 says the byte range
/// started in the wrong place.
///
/// # Errors
///
/// Returns [`GribError`] when the bytes are not a GRIB2 message or a section
/// runs past the end of one.
pub fn section_lengths(bytes: &[u8]) -> Result<Vec<(u8, usize)>, GribError> {
    let body = indicator(bytes)?;

    Ok(section::walk(body)?
        .into_iter()
        .map(|section| (section.number, section.body.len()))
        .collect())
}

/// Validates section 0 and returns the message, trimmed to its stated length.
fn indicator(bytes: &[u8]) -> Result<&[u8], GribError> {
    if bytes.len() < INDICATOR_LEN {
        return Err(GribError::Truncated {
            what: "the indicator section",
            needed: INDICATOR_LEN,
            available: bytes.len(),
        });
    }

    if &bytes[0..4] != b"GRIB" {
        return Err(GribError::NotGrib);
    }

    let edition = bytes[7];

    if edition != 2 {
        return Err(GribError::UnsupportedEdition(edition));
    }

    let total = usize::try_from(section::be_u64(bytes, 8)).unwrap_or(usize::MAX);

    if bytes.len() < total {
        return Err(GribError::Truncated {
            what: "the message body",
            needed: total,
            available: bytes.len(),
        });
    }

    Ok(&bytes[..total])
}

/// What a message is, without decoding a single value.
///
/// Everything needed to confirm a fetched message is the field that was asked
/// for, which is the check the ingest pipeline runs before spending anything
/// on the data section.
#[derive(Debug, Clone, Copy, PartialEq)]
pub struct Header {
    /// The GRIB2 discipline. Zero is meteorological products.
    pub discipline: u8,

    /// Where the values sit on the earth.
    pub grid: Grid,

    /// What the values are.
    pub product: Product,
}

/// Reads a message's header without unpacking its values.
///
/// Separate from a full decode because the two are wanted at different
/// moments and cost different amounts. A fetcher locating a message by its
/// index abbreviation needs to know the bytes that arrived carry category 2,
/// number 22 at level type 1 — and finding out they do not should not require
/// unpacking two million points first.
///
/// # Errors
///
/// Returns [`GribError`] naming the unsupported template or the shortfall.
pub fn header(bytes: &[u8]) -> Result<Header, GribError> {
    let body = indicator(bytes)?;

    let mut grid = None;
    let mut product = None;

    for section in section::walk(body)? {
        match section.number {
            3 => grid = Some(grid::parse(section.body)?),
            4 => product = Some(product::parse(section.body)?),
            _ => {}
        }
    }

    Ok(Header {
        discipline: bytes[6],
        grid: grid.ok_or(GribError::MissingSection(3))?,
        product: product.ok_or(GribError::MissingSection(4))?,
    })
}

/// A decoded GRIB2 message: one field, on one grid, at one level and time.
#[derive(Debug, Clone)]
pub struct Message {
    /// What the message is, without its values.
    pub header: Header,

    /// The values, in grid order: `values[j * nx + i]`.
    ///
    /// Always `nx * ny` long, including for a constant field whose data
    /// section is empty — a caller indexing by position must not have to ask
    /// which packing produced it.
    pub values: Vec<f64>,
}

/// Reads one GRIB2 message, values and all.
///
/// # Errors
///
/// Returns a [`GribError`] naming the template, section or shortfall rather
/// than a generic parse failure — see the type's documentation for why.
pub fn parse(bytes: &[u8]) -> Result<Message, GribError> {
    let body = indicator(bytes)?;

    let mut grid = None;
    let mut product = None;
    let mut packing = None;
    let mut data: Option<&[u8]> = None;

    for section in section::walk(body)? {
        match section.number {
            3 => grid = Some(grid::parse(section.body)?),
            4 => product = Some(product::parse(section.body)?),
            5 => packing = Some(unpack::parse(section.body)?),
            6 => {
                // 255 means "no bitmap applies", which is what every product
                // this reads emits. Anything else is a real bitmap, and
                // decoding values without knowing which points they belong to
                // would misplace every one of them.
                let indicator = section.body[section::BITMAP_INDICATOR_AT];

                if indicator != section::NO_BITMAP {
                    return Err(GribError::BitmapUnsupported(indicator));
                }
            }
            7 => data = Some(&section.body[section::DATA_PAYLOAD_AT..]),
            _ => {}
        }
    }

    let grid = grid.ok_or(GribError::MissingSection(3))?;
    let product = product.ok_or(GribError::MissingSection(4))?;
    let packing = packing.ok_or(GribError::MissingSection(5))?;
    let data = data.ok_or(GribError::MissingSection(7))?;

    let expected = grid.point_count();
    let values = unpack::decode(&packing, data, expected);

    if values.len() != expected {
        return Err(GribError::ValueCountMismatch {
            decoded: values.len(),
            expected,
        });
    }

    Ok(Message {
        header: Header {
            discipline: bytes[6],
            grid,
            product,
        },
        values,
    })
}
