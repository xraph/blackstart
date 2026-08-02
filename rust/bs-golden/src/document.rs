//! The on-disk golden format.
//!
//! Deliberately a separate set of types from `bs_case::Case`. The file format
//! is a contract with a Python generator and has to stay stable; the internal
//! model is free to change. Deriving serialization straight onto the solver's
//! own types would tie the two together, so that a field rename in the
//! numerics silently invalidates every committed golden.

use std::fs;
use std::path::{Path, PathBuf};

use bs_case::{Branch, Bus, BusKind, Case};
use bs_quantity::Base;
use serde::Deserialize;

/// One golden file: provenance, the network, and the oracle's answer.
#[derive(Debug, Clone, Deserialize)]
pub struct Golden {
    /// Where this file came from. Read and reported rather than ignored,
    /// because a golden that cannot say what produced it is not evidence.
    pub provenance: Provenance,
    /// The network, in blackstart's per-unit representation.
    pub case: CaseDocument,
    /// The oracle's solution.
    pub reference: Reference,
}

/// What produced a golden.
#[derive(Debug, Clone, Deserialize)]
pub struct Provenance {
    /// The script that wrote it.
    pub generator: String,
    /// The commit it was written at, or a `-dirty` marker.
    pub generator_commit: String,
    /// The tool that solved it.
    pub oracle: String,
    /// That tool's version.
    pub oracle_version: String,
    /// Which power-flow method the oracle used.
    pub method: String,
    /// Whether the oracle enforced generator reactive limits.
    ///
    /// Recorded because it decides which of blackstart's solves the reference
    /// may be compared against. A limited and an unlimited solve answer
    /// different questions, and comparing one against the other produces a
    /// disagreement that looks like a bug and is not.
    pub enforce_q_lims: bool,
    /// The network's name.
    pub case: String,
}

impl Provenance {
    /// Whether this golden can be traced to a commit.
    ///
    /// A golden generated from an uncommitted tree describes code that no
    /// longer exists anywhere retrievable. It is still useful for local
    /// iteration and must not be committed, which is what CI checks.
    #[must_use]
    pub fn is_traceable(&self) -> bool {
        !self.generator_commit.is_empty()
            && self.generator_commit != "unknown"
            && !self.generator_commit.ends_with("-dirty")
    }
}

/// The per-unit base.
#[derive(Debug, Clone, Copy, Deserialize)]
pub struct BaseDocument {
    /// Apparent power base, MVA.
    pub s_base_mva: f64,
    /// Voltage base, kV.
    pub v_base_kv: f64,
}

/// A bus, as written in the file.
#[derive(Debug, Clone, Deserialize)]
pub struct BusDocument {
    /// Stable identifier.
    pub id: String,
    /// `pq`, `pv` or `slack`.
    pub kind: String,
    /// Net real power injection, per unit.
    pub p_injection: f64,
    /// Net reactive power injection, per unit.
    pub q_injection: f64,
    /// Voltage magnitude, per unit.
    pub v_magnitude: f64,
    /// Voltage angle, radians.
    pub v_angle: f64,
    /// Lower reactive limit, or null for unlimited.
    ///
    /// JSON has no infinity, and null is the better encoding regardless: a
    /// generator with no stated limits has unknown limits, and a magic large
    /// number would be indistinguishable from a real one.
    pub q_min: Option<f64>,
    /// Upper reactive limit, or null for unlimited.
    pub q_max: Option<f64>,
    /// Shunt conductance, per unit.
    pub g_shunt: f64,
    /// Shunt susceptance, per unit.
    pub b_shunt: f64,
}

/// A branch, as written in the file.
#[derive(Debug, Clone, Deserialize)]
pub struct BranchDocument {
    /// Stable identifier.
    pub id: String,
    /// From-bus index.
    pub from: usize,
    /// To-bus index.
    pub to: usize,
    /// Series resistance, per unit.
    pub r: f64,
    /// Series reactance, per unit.
    pub x: f64,
    /// Total charging susceptance, per unit.
    pub b: f64,
    /// Long-term rating, per unit. Zero means unrated.
    pub rating: f64,
    /// Whether the branch is in service.
    pub in_service: bool,
    /// Off-nominal turns ratio.
    pub tap: f64,
    /// Phase shift, radians.
    pub shift: f64,
}

/// A network, as written in the file.
#[derive(Debug, Clone, Deserialize)]
pub struct CaseDocument {
    /// The network's name.
    pub name: String,
    /// The per-unit base.
    pub base: BaseDocument,
    /// Buses in index order.
    pub buses: Vec<BusDocument>,
    /// Branches in index order.
    pub branches: Vec<BranchDocument>,
}

/// The oracle's answer.
#[derive(Debug, Clone, Deserialize)]
pub struct Reference {
    /// Bus voltage magnitudes, per unit.
    pub v_magnitude: Vec<f64>,
    /// Bus voltage angles, radians.
    pub v_angle: Vec<f64>,
}

/// Why a golden could not be loaded.
#[derive(Debug)]
pub enum LoadError {
    /// The file could not be read.
    Io(PathBuf, std::io::Error),
    /// The file is not valid JSON, or does not match the format.
    Parse(PathBuf, serde_json::Error),
    /// The file parsed but does not describe a solvable network.
    Invalid(PathBuf, String),
}

impl core::fmt::Display for LoadError {
    fn fmt(&self, f: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        match self {
            Self::Io(path, e) => write!(f, "reading {}: {e}", path.display()),
            Self::Parse(path, e) => write!(f, "parsing {}: {e}", path.display()),
            Self::Invalid(path, why) => write!(f, "{}: {why}", path.display()),
        }
    }
}

impl std::error::Error for LoadError {}

impl Golden {
    /// Builds the solvable case this golden describes.
    ///
    /// # Errors
    ///
    /// Returns a message when the base is invalid or the network is not
    /// structurally solvable.
    pub fn to_case(&self) -> Result<Case, String> {
        let base = Base::new(self.case.base.s_base_mva, self.case.base.v_base_kv)
            .map_err(|e| format!("per-unit base: {e}"))?;

        let mut buses = Vec::with_capacity(self.case.buses.len());

        for (index, doc) in self.case.buses.iter().enumerate() {
            let kind = match doc.kind.as_str() {
                "pq" => BusKind::Pq,
                "pv" => BusKind::Pv,
                "slack" => BusKind::Slack,
                other => return Err(format!("bus {index}: unknown kind {other:?}")),
            };

            buses.push(Bus {
                id: doc.id.clone(),
                kind,
                p_injection: doc.p_injection,
                q_injection: doc.q_injection,
                v_magnitude: doc.v_magnitude,
                v_angle: doc.v_angle,
                q_min: doc.q_min.unwrap_or(f64::NEG_INFINITY),
                q_max: doc.q_max.unwrap_or(f64::INFINITY),
                g_shunt: doc.g_shunt,
                b_shunt: doc.b_shunt,
            });
        }

        let branches = self
            .case
            .branches
            .iter()
            .map(|doc| Branch {
                id: doc.id.clone(),
                from: doc.from,
                to: doc.to,
                r: doc.r,
                x: doc.x,
                b: doc.b,
                rating: doc.rating,
                in_service: doc.in_service,
                tap: doc.tap,
                shift: doc.shift,
            })
            .collect();

        let case = Case::new(self.case.name.clone(), base, buses, branches);
        case.validate().map_err(|e| e.to_string())?;

        Ok(case)
    }
}

/// Loads one golden file.
///
/// # Errors
///
/// Returns [`LoadError`] when the file cannot be read, parsed, or does not
/// describe a solvable network.
pub fn load(path: impl AsRef<Path>) -> Result<Golden, LoadError> {
    let path = path.as_ref();

    let body = fs::read_to_string(path).map_err(|e| LoadError::Io(path.to_path_buf(), e))?;
    let golden: Golden =
        serde_json::from_str(&body).map_err(|e| LoadError::Parse(path.to_path_buf(), e))?;

    // Checked here rather than at every use site: a reference vector of the
    // wrong length would compare against whatever happened to line up, which
    // is worse than not comparing at all.
    if golden.reference.v_magnitude.len() != golden.case.buses.len()
        || golden.reference.v_angle.len() != golden.case.buses.len()
    {
        return Err(LoadError::Invalid(
            path.to_path_buf(),
            format!(
                "the reference has {} magnitudes and {} angles for {} buses",
                golden.reference.v_magnitude.len(),
                golden.reference.v_angle.len(),
                golden.case.buses.len()
            ),
        ));
    }

    Ok(golden)
}

/// Loads every `*.golden.json` in a directory, in sorted filename order.
///
/// Sorted so a failure report reads the same way every run, which matters when
/// the report is the only artefact of a CI job.
///
/// # Errors
///
/// Returns [`LoadError`] for the first file that cannot be loaded.
pub fn load_all(dir: impl AsRef<Path>) -> Result<Vec<(PathBuf, Golden)>, LoadError> {
    let dir = dir.as_ref();

    let entries = fs::read_dir(dir).map_err(|e| LoadError::Io(dir.to_path_buf(), e))?;

    let mut paths: Vec<PathBuf> = entries
        .filter_map(Result::ok)
        .map(|entry| entry.path())
        .filter(|path| {
            path.file_name()
                .and_then(|name| name.to_str())
                .is_some_and(|name| name.ends_with(".golden.json"))
        })
        .collect();

    paths.sort();

    paths
        .into_iter()
        .map(|path| load(&path).map(|golden| (path, golden)))
        .collect()
}
