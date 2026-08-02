//! Validation against pandapower.
//!
//! Every other check in this workspace is internal — the solver against its
//! own invariants, the screen against the solve, AC against DC in the linear
//! limit. Those are strong, and they share one blind spot: a misunderstanding
//! held consistently everywhere. A sign convention, a per-unit base, a
//! transformer applied at the wrong end. Only an independent implementation
//! can see that, which is what this file is for.
//!
//! It is the complement of the oracle-independent tests, not a replacement.
//! Those catch a golden regenerated from broken code; this catches code that
//! is self-consistently wrong.

use std::path::PathBuf;

use bs_golden::{Tolerances, compare, load_all};
use bs_powerflow::ac::{Options, Voltages, solve_ac_from};

fn golden_dir() -> PathBuf {
    // The workspace lives in rust/, the goldens in data/ at the repository
    // root, so this walks up two levels from the crate.
    PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join("../../data/golden")
        .canonicalize()
        .expect("data/golden should exist; run tools/goldengen/generate.py")
}

/// Solve every committed golden and require agreement with pandapower.
///
/// Reactive limits are *not* enforced here, because the goldens record that
/// the oracle did not enforce them either. A limited and an unlimited solve
/// answer different questions, and comparing one against the other produces a
/// disagreement that looks like a bug and is not — which is exactly the wrong
/// turn this test took on its first run.
#[test]
fn every_golden_matches_pandapower() {
    let goldens = load_all(golden_dir()).expect("goldens should load");

    assert!(
        !goldens.is_empty(),
        "no goldens found; the comparison would pass vacuously"
    );

    let tolerances = Tolerances::default();
    let mut failures = Vec::new();

    for (path, golden) in &goldens {
        let case = golden
            .to_case()
            .unwrap_or_else(|e| panic!("{}: {e}", path.display()));

        assert!(
            !golden.provenance.enforce_q_lims,
            "{}: the oracle enforced reactive limits, so this unlimited \
             comparison is against a different problem",
            golden.provenance.case
        );

        let solved = match solve_ac_from(&case, &Voltages::flat(&case), Options::default()) {
            Ok(solution) => solution,
            Err(e) => {
                failures.push(format!("{}: did not solve: {e}", golden.provenance.case));
                continue;
            }
        };

        let comparison = compare(&solved.voltages, &golden.reference, tolerances);

        if !comparison.agrees() {
            failures.push(format!(
                "{} (oracle {} {}): {}",
                golden.provenance.case,
                golden.provenance.oracle,
                golden.provenance.oracle_version,
                comparison.report(5)
            ));
        }
    }

    assert!(
        failures.is_empty(),
        "disagreement with pandapower:\n{}",
        failures.join("\n")
    );
}

/// The margin, reported rather than merely passed. If the worst error ever
/// creeps toward the tolerance, this test has stopped proving much and the
/// failure says so before a silent drift becomes a silent regression.
#[test]
fn agreement_is_comfortable_not_marginal() {
    let goldens = load_all(golden_dir()).expect("goldens should load");
    let tolerances = Tolerances::default();

    for (_, golden) in &goldens {
        let Ok(case) = golden.to_case() else { continue };
        let Ok(solved) = solve_ac_from(&case, &Voltages::flat(&case), Options::default()) else {
            continue;
        };

        let comparison = compare(&solved.voltages, &golden.reference, tolerances);

        assert!(
            comparison.worst_magnitude < tolerances.magnitude / 10.0,
            "{}: worst magnitude error {:.3e} is within one order of the {:.3e} \
             tolerance; the margin has eroded",
            golden.provenance.case,
            comparison.worst_magnitude,
            tolerances.magnitude
        );
        assert!(
            comparison.worst_angle < tolerances.angle / 10.0,
            "{}: worst angle error {:.3e} is within one order of the {:.3e} \
             tolerance",
            golden.provenance.case,
            comparison.worst_angle,
            tolerances.angle
        );
    }
}

/// A golden generated from an uncommitted tree describes code that cannot be
/// retrieved. It is fine locally and must never be committed, because the
/// chain from a number back to the code that produced it breaks at its first
/// link.
#[test]
fn every_committed_golden_is_traceable_to_a_commit() {
    for (path, golden) in load_all(golden_dir()).expect("goldens should load") {
        assert!(
            golden.provenance.is_traceable(),
            "{} was generated at {:?}; regenerate it from a clean tree",
            path.display(),
            golden.provenance.generator_commit
        );
    }
}

/// The provenance header is the reason a golden is evidence rather than a
/// number someone wrote down. Every field has to be populated.
#[test]
fn every_golden_says_what_produced_it() {
    for (path, golden) in load_all(golden_dir()).expect("goldens should load") {
        let provenance = &golden.provenance;
        let where_ = path.display();

        assert!(!provenance.generator.is_empty(), "{where_}: no generator");
        assert!(!provenance.oracle.is_empty(), "{where_}: no oracle");
        assert!(
            !provenance.oracle_version.is_empty(),
            "{where_}: no oracle version — 'pandapower said so' is not \
             reproducible without knowing which pandapower"
        );
        assert!(!provenance.method.is_empty(), "{where_}: no method");
        assert!(!provenance.case.is_empty(), "{where_}: no case name");
    }
}

/// The whole point of ranking disagreements is that the first few lines say
/// what is wrong. A 300-bus network dumped in full is a report nobody reads,
/// which is the same as no report.
#[test]
fn a_failure_report_is_short_and_worst_first() {
    let goldens = load_all(golden_dir()).expect("goldens should load");
    let (_, golden) = goldens.first().expect("at least one golden");

    let case = golden.to_case().unwrap();
    let solved = solve_ac_from(&case, &Voltages::flat(&case), Options::default()).unwrap();

    // An impossible tolerance, so everything disagrees and the report is
    // exercised on its worst case rather than on the empty one.
    let comparison = compare(
        &solved.voltages,
        &golden.reference,
        Tolerances {
            magnitude: 0.0,
            angle: 0.0,
        },
    );

    assert!(!comparison.agrees());

    let report = comparison.report(3);
    assert!(
        report.lines().count() <= 5,
        "the report ran long:\n{report}"
    );
    assert!(
        report.contains("and"),
        "the report did not say what it omitted"
    );

    for pair in comparison.disagreements.windows(2) {
        assert!(
            pair[0].error >= pair[1].error,
            "disagreements are not worst-first"
        );
    }
}

/// Every golden must describe a network the solver will accept, and the ones
/// committed are the standard IEEE cases the plan names.
#[test]
fn the_committed_goldens_cover_the_intended_networks() {
    let goldens = load_all(golden_dir()).expect("goldens should load");

    let names: Vec<&str> = goldens
        .iter()
        .map(|(_, g)| g.provenance.case.as_str())
        .collect();

    for expected in ["case14", "case30", "case57", "case118", "case300"] {
        assert!(
            names.contains(&expected),
            "{expected} is missing; committed goldens are {names:?}"
        );
    }

    // And each one is a real network rather than an empty document.
    for (path, golden) in &goldens {
        let case = golden.to_case().unwrap();
        assert!(
            case.n_bus() >= 14,
            "{}: only {} buses",
            path.display(),
            case.n_bus()
        );
        assert!(case.n_branch() > 0, "{}: no branches", path.display());
    }
}
