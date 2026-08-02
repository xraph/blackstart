//! The content-addressed model cache.
//!
//! A contingency sweep solves one network thousands of times, so the network
//! is uploaded once and named by the sha256 of its serialized bytes
//! thereafter. Content addressing rather than a server-assigned identifier is
//! what makes that safe: two clients that computed the same model get the same
//! name without coordinating, and a client cannot be handed a stale handle
//! that now points at something else.

use std::collections::BTreeMap;
use std::sync::RwLock;

use bs_case::Case;
use sha2::{Digest, Sha256};

use crate::proto;

/// The hex-encoded sha256 of a serialized model.
#[must_use]
pub fn content_hash(bytes: &[u8]) -> String {
    let digest = Sha256::digest(bytes);

    // Lowercase hex, written out rather than pulled from a crate: this is the
    // one place it happens and the contract fixes the encoding.
    digest.iter().fold(String::with_capacity(64), |mut out, b| {
        use core::fmt::Write as _;
        let _ = write!(out, "{b:02x}");
        out
    })
}

/// A model the server has, decoded once.
#[derive(Debug, Clone)]
pub struct Entry {
    /// The wire form, kept because results have to be converted back into the
    /// SI units and base it declared.
    pub model: proto::NetworkModel,
    /// The per-unit case, converted once at upload rather than per solve.
    pub case: Case,
}

/// Models held by content hash.
///
/// A `BTreeMap` rather than a `HashMap`: nothing here is on a numeric path, but
/// ordered iteration makes the cache's contents reproducible in a log or a
/// test, and the map is small enough that the difference costs nothing.
#[derive(Debug, Default)]
pub struct ModelCache {
    entries: RwLock<BTreeMap<String, Entry>>,
}

impl ModelCache {
    /// An empty cache.
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }

    /// Whether a model is present.
    ///
    /// # Panics
    ///
    /// Panics if the lock is poisoned, which means a previous holder panicked
    /// while mutating the cache. Continuing with possibly-torn state would be
    /// worse than stopping.
    #[must_use]
    pub fn contains(&self, hash: &str) -> bool {
        self.entries.read().expect("cache lock").contains_key(hash)
    }

    /// Stores a model, returning whether it was already present.
    ///
    /// # Panics
    ///
    /// Panics if the lock is poisoned.
    pub fn insert(&self, hash: String, entry: Entry) -> bool {
        self.entries
            .write()
            .expect("cache lock")
            .insert(hash, entry)
            .is_some()
    }

    /// Retrieves a model.
    ///
    /// Returns a clone rather than a guard so that a long solve does not hold
    /// the lock: a sweep is seconds of arithmetic, and blocking every other
    /// request for its duration would make the cache the bottleneck it exists
    /// to remove.
    ///
    /// # Panics
    ///
    /// Panics if the lock is poisoned.
    #[must_use]
    pub fn get(&self, hash: &str) -> Option<Entry> {
        self.entries.read().expect("cache lock").get(hash).cloned()
    }

    /// How many models are held.
    ///
    /// # Panics
    ///
    /// Panics if the lock is poisoned.
    #[must_use]
    pub fn len(&self) -> usize {
        self.entries.read().expect("cache lock").len()
    }

    /// Whether the cache is empty.
    ///
    /// # Panics
    ///
    /// Panics if the lock is poisoned.
    #[must_use]
    pub fn is_empty(&self) -> bool {
        self.len() == 0
    }
}
