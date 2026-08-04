//! The weather grid service: decode GRIB2 once, sample it many times.
//!
//! Lives in this crate rather than its own because it shares the binary. One
//! Rust process serves both contracts — a second would mean a second
//! deployment, a second health check and a second thing to be down, for two
//! services that are each a thin front end over a library.
//!
//! The shape mirrors the solver's for the same reason it has that shape: a
//! payload that is expensive to prepare and cheap to query is uploaded once,
//! named by the hash of its own bytes, and referenced thereafter. Here the
//! expensive part is decoding 1.9 million points out of a complex-packed
//! message, and the cheap part is a projection and four multiplications per
//! sample.

use std::collections::BTreeMap;
use std::sync::RwLock;

use bs_grib::{Grid, Message};
use tonic::{Request, Response, Status, Streaming};

use crate::cache::content_hash;
use crate::weather_proto as proto;

/// Grids held by content hash.
///
/// A `BTreeMap` rather than a `HashMap`, matching the model cache: nothing
/// here is on a numeric path, but ordered iteration makes the cache's contents
/// reproducible in a log or a dump.
#[derive(Debug, Default)]
pub struct GridCache {
    entries: RwLock<BTreeMap<String, Message>>,
}

impl GridCache {
    /// An empty cache.
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }

    /// Whether a grid is cached.
    #[must_use]
    pub fn contains(&self, hash: &str) -> bool {
        self.entries
            .read()
            .is_ok_and(|entries| entries.contains_key(hash))
    }

    /// Runs a closure over a cached grid, if it is there.
    fn with<T>(&self, hash: &str, f: impl FnOnce(&Message) -> T) -> Option<T> {
        self.entries.read().ok()?.get(hash).map(f)
    }

    /// Stores a decoded grid, reporting whether it was already held.
    fn insert(&self, hash: String, message: Message) -> bool {
        let Ok(mut entries) = self.entries.write() else {
            return false;
        };

        entries.insert(hash, message).is_some()
    }
}

/// The service.
#[derive(Debug, Default)]
pub struct WeatherGrid {
    cache: GridCache,
}

impl WeatherGrid {
    /// A service with an empty cache.
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }
}

/// Describes a decoded message for the wire.
fn describe(message: &Message) -> proto::GridDescription {
    let (nx, ny) = message.header.grid.shape();

    let projection = match message.header.grid {
        Grid::LatLon { .. } => "latlon",
        Grid::Lambert { .. } => "lambert",
    };

    proto::GridDescription {
        category: u32::from(message.header.product.category),
        number: u32::from(message.header.product.number),
        level_type: u32::from(message.header.product.level_type),
        level_value: message.header.product.level_value,
        nx: u32::try_from(nx).unwrap_or(u32::MAX),
        ny: u32::try_from(ny).unwrap_or(u32::MAX),
        projection: projection.to_owned(),
    }
}

#[tonic::async_trait]
impl proto::weather_grid_service_server::WeatherGridService for WeatherGrid {
    async fn put_grid(
        &self,
        request: Request<Streaming<proto::PutGridRequest>>,
    ) -> Result<Response<proto::PutGridResponse>, Status> {
        let mut stream = request.into_inner();
        let mut header: Option<proto::PutGridHeader> = None;
        let mut bytes: Vec<u8> = Vec::new();

        while let Some(message) = stream.message().await? {
            match message.payload {
                Some(proto::put_grid_request::Payload::Header(sent)) => {
                    if header.is_some() {
                        return Err(Status::invalid_argument(
                            "a second header arrived mid-stream; the header is sent once",
                        ));
                    }

                    // Reserving up front is the header's other purpose: a
                    // multi-megabyte message grown a chunk at a time reallocates
                    // repeatedly for no reason.
                    bytes.reserve(usize::try_from(sent.size_bytes).unwrap_or(0));

                    // A grid already held needs no upload. Answering before the
                    // chunks arrive is the whole reason the hash is sent first.
                    if self.cache.contains(&sent.content_hash) {
                        let description = self
                            .cache
                            .with(&sent.content_hash, describe)
                            .unwrap_or_default();

                        return Ok(Response::new(proto::PutGridResponse {
                            content_hash: sent.content_hash,
                            already_present: true,
                            description: Some(description),
                        }));
                    }

                    header = Some(sent);
                }
                Some(proto::put_grid_request::Payload::Chunk(chunk)) => {
                    if header.is_none() {
                        return Err(Status::invalid_argument(
                            "a chunk arrived before the header; the hash has to come first \
                             so the server can verify as it receives",
                        ));
                    }

                    bytes.extend_from_slice(&chunk);
                }
                None => {}
            }
        }

        let header =
            header.ok_or_else(|| Status::invalid_argument("the stream ended without a header"))?;

        if bytes.len() as u64 != header.size_bytes {
            return Err(Status::invalid_argument(format!(
                "the stream promised {} bytes and delivered {}",
                header.size_bytes,
                bytes.len()
            )));
        }

        let computed = content_hash(&bytes);

        if computed != header.content_hash {
            return Err(Status::data_loss(format!(
                "the bytes hash to {computed} and the header said {}; something \
                 altered them in flight, and a grid stored under the wrong hash \
                 would be sampled for a retrieval it did not come from",
                header.content_hash
            )));
        }

        // Decoding here rather than at first sample is deliberate: a message
        // this server cannot read should fail the upload, where the caller is
        // still holding the bytes and can say which retrieval they came from.
        let message = bs_grib::parse(&bytes)
            .map_err(|e| Status::invalid_argument(format!("decoding {computed}: {e}")))?;

        let description = describe(&message);
        let already_present = self.cache.insert(computed.clone(), message);

        Ok(Response::new(proto::PutGridResponse {
            content_hash: computed,
            already_present,
            description: Some(description),
        }))
    }

    async fn has_grid(
        &self,
        request: Request<proto::HasGridRequest>,
    ) -> Result<Response<proto::HasGridResponse>, Status> {
        let hash = request.into_inner().content_hash;
        let description = self.cache.with(&hash, describe);

        Ok(Response::new(proto::HasGridResponse {
            present: description.is_some(),
            description,
        }))
    }

    async fn sample_grid(
        &self,
        request: Request<proto::SampleGridRequest>,
    ) -> Result<Response<proto::SampleGridResponse>, Status> {
        let request = request.into_inner();

        let samples = self
            .cache
            .with(&request.content_hash, |message| {
                request
                    .points
                    .iter()
                    .map(|point| match message.sample(point.lon, point.lat) {
                        // Absence is absence. A point outside the domain is the
                        // ordinary case for any span outside CONUS, not an
                        // error — and not a zero, which would be a fabricated
                        // calm entering a hazard calculation.
                        None => proto::Sample {
                            present: false,
                            value: 0.0,
                        },
                        Some(value) => proto::Sample {
                            present: true,
                            value,
                        },
                    })
                    .collect::<Vec<_>>()
            })
            .ok_or_else(|| {
                Status::failed_precondition(format!(
                    "grid {} is not cached; upload it before sampling",
                    request.content_hash
                ))
            })?;

        Ok(Response::new(proto::SampleGridResponse { samples }))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn an_empty_cache_holds_nothing() {
        let cache = GridCache::new();

        assert!(!cache.contains("sha256:whatever"));
        assert!(cache.with("sha256:whatever", |_| ()).is_none());
    }

    #[test]
    fn a_lambert_grid_describes_itself_as_lambert() {
        let path = std::path::Path::new(env!("CARGO_MANIFEST_DIR"))
            .join("../bs-grib/tests/fixtures/hrrr_ltng_atmos.grib2");

        let bytes = std::fs::read(path).expect("the HRRR fixture");
        let message = bs_grib::parse(&bytes).expect("parsing");

        let described = describe(&message);

        assert_eq!(described.projection, "lambert");
        assert_eq!((described.nx, described.ny), (1799, 1059));
        assert_eq!(described.category, 17);
        assert_eq!(described.number, 192);
    }

    #[test]
    fn inserting_twice_reports_the_second_as_present() {
        let path = std::path::Path::new(env!("CARGO_MANIFEST_DIR"))
            .join("../bs-grib/tests/fixtures/hrrr_cicep_surface.grib2");

        let bytes = std::fs::read(path).expect("the CICEP fixture");
        let message = bs_grib::parse(&bytes).expect("parsing");

        let cache = GridCache::new();

        assert!(!cache.insert("abc".to_owned(), message.clone()));
        assert!(cache.insert("abc".to_owned(), message));
        assert!(cache.contains("abc"));
    }
}
