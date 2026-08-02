//! Full AC verification of screened contingencies.
//!
//! The screen ranks; this decides. What is checked here is that verification
//! genuinely sees things the screen cannot — voltage violations, which DC
//! cannot represent at all because it holds every magnitude at 1.0 — and that
//! the outcomes a caller acts on cannot be quietly confused with each other.
//!
//! The one that matters most is Insolvable. A contingency the power flow
//! cannot solve is the most serious result available, not the least, and a
//! type or a helper that lets it read as "nothing found" would invert its
//! meaning.

use bs_case::{Branch, Bus, Case};
use bs_contingency::verify::{Verdict, VoltageLimits, verify, verify_top};
use bs_contingency::{Contingency, all_branches, rank, screen};
use bs_factors::Lodf;
use bs_powerflow::ac::Options;
use bs_powerflow::solve_dc;
use bs_quantity::Base;

fn base() -> Base {
    Base::new(100.0, 138.0).unwrap()
}

/// A meshed network with generous ratings, so nothing is thermally violated
/// and any finding is genuinely about voltage.
fn healthy() -> Case {
    Case::new(
        "healthy",
        base(),
        vec![
            Bus::slack("s", 1.0),
            Bus::load("a", -0.2, -0.05),
            Bus::load("b", -0.3, -0.08),
            Bus::generator("g", 0.2, 1.0),
        ],
        vec![
            Branch::new("s-a", 0, 1, 0.01, 0.10).with_rating(5.0),
            Branch::new("a-b", 1, 2, 0.01, 0.15).with_rating(5.0),
            Branch::new("b-g", 2, 3, 0.01, 0.12).with_rating(5.0),
            Branch::new("g-s", 3, 0, 0.01, 0.08).with_rating(5.0),
        ],
    )
}

#[test]
fn a_secure_contingency_is_reported_as_secure() {
    let case = healthy();
    let result = verify(
        &case,
        &Contingency::new("s-a", 0),
        VoltageLimits::default(),
        Options::default(),
    );

    assert_eq!(result.verdict, Verdict::Secure);
    assert!(!result.verdict.is_concerning());
    assert!(
        result.solution.is_some(),
        "a secure verdict discarded its solution"
    );
}

/// A thermal overload the screen would also find, confirmed by AC.
#[test]
fn a_thermal_overload_is_reported_with_its_loading() {
    let mut case = healthy();
    // Tight enough that losing one path overloads the other.
    for branch in &mut case.branches {
        branch.rating = 0.25;
    }

    let result = verify(
        &case,
        &Contingency::new("a-b", 1),
        VoltageLimits::default(),
        Options::default(),
    );

    let Verdict::Violated { thermal, .. } = &result.verdict else {
        panic!("expected a violation, got {:?}", result.verdict);
    };

    assert!(!thermal.is_empty());
    for violation in thermal {
        assert!(violation.loading > 1.0);
        assert!((violation.loading - violation.flow.abs() / violation.rating).abs() < 1e-12);
        // The outaged branch itself carries nothing and must not be reported.
        assert_ne!(violation.branch, 1);
    }
}

/// The reason verification exists. DC holds every voltage at 1.0, so a voltage
/// violation is invisible to the screen by construction — no amount of
/// refinement to the DC tier would find this.
#[test]
fn verification_finds_voltage_violations_the_screen_cannot() {
    // Heavy reactive load behind a weak generator, with generous thermal
    // ratings so nothing else can be responsible for the finding.
    let case = Case::new(
        "weak",
        base(),
        vec![
            Bus::slack("s", 1.0),
            Bus::load("a", -0.5, -0.45),
            Bus::load("b", -0.6, -0.50),
            Bus::generator("g", 0.4, 1.0).with_reactive_limits(-0.1, 0.1),
        ],
        vec![
            Branch::new("s-a", 0, 1, 0.03, 0.18).with_rating(9.0),
            Branch::new("a-b", 1, 2, 0.04, 0.22).with_rating(9.0),
            Branch::new("b-g", 2, 3, 0.03, 0.20).with_rating(9.0),
            Branch::new("g-s", 3, 0, 0.02, 0.14).with_rating(9.0),
        ],
    );

    // Nothing thermal in the DC screen, so anything found below is voltage.
    let pre = solve_dc(&case).unwrap();
    let lodf = Lodf::build(&case).unwrap();
    let screened = screen(&case, &lodf, &pre.flows, &all_branches(&case), 1.0).unwrap();
    assert!(
        screened.iter().all(|r| !r.has_violations()),
        "the screen found a thermal problem; this test would prove nothing"
    );

    let verified = verify_top(
        &case,
        &rank(screened),
        4,
        VoltageLimits::default(),
        Options::default(),
    );

    let voltage_findings: usize = verified
        .iter()
        .filter(|v| match &v.verdict {
            Verdict::Violated { voltage, .. } => !voltage.is_empty(),
            Verdict::Insolvable(_) => true,
            Verdict::Secure => false,
        })
        .count();

    assert!(
        voltage_findings > 0,
        "AC verification found nothing the DC screen missed; either the case \
         is not stressed enough or verification is not looking at voltage"
    );

    // And every reported violation really is outside the band.
    for result in &verified {
        if let Verdict::Violated { voltage, .. } = &result.verdict {
            for violation in voltage {
                assert!(
                    violation.magnitude < 0.95 || violation.magnitude > 1.05,
                    "bus {} at {} was reported but is inside the band",
                    violation.bus,
                    violation.magnitude
                );
            }
        }
    }
}

/// A voltage violation caused by a generator running out of reactive power
/// must report the generator too. Reporting the symptom without the cause
/// sends an operator looking in the wrong place.
#[test]
fn a_voltage_violation_names_the_generators_that_ran_out() {
    let case = Case::new(
        "weak",
        base(),
        vec![
            Bus::slack("s", 1.0),
            Bus::load("a", -0.5, -0.45),
            Bus::load("b", -0.6, -0.50),
            Bus::generator("g", 0.4, 1.05).with_reactive_limits(-0.05, 0.05),
        ],
        vec![
            Branch::new("s-a", 0, 1, 0.03, 0.18).with_rating(9.0),
            Branch::new("a-b", 1, 2, 0.04, 0.22).with_rating(9.0),
            Branch::new("b-g", 2, 3, 0.03, 0.20).with_rating(9.0),
            Branch::new("g-s", 3, 0, 0.02, 0.14).with_rating(9.0),
        ],
    );

    let result = verify(
        &case,
        &Contingency::new("s-a", 0),
        VoltageLimits::default(),
        Options::default(),
    );

    if let Verdict::Violated {
        voltage,
        binding_generators,
        ..
    } = &result.verdict
        && !voltage.is_empty()
    {
        assert!(
            binding_generators.contains(&3),
            "the voltage collapsed but the generator holding it was not named"
        );
    }
}

/// Voltage limits are configuration, not constants. A tool that hard-codes an
/// acceptable band is asserting a policy it has no standing to assert.
#[test]
fn the_voltage_band_is_the_callers_choice() {
    let case = healthy();
    let contingency = Contingency::new("s-a", 0);

    let normal = verify(
        &case,
        &contingency,
        VoltageLimits::default(),
        Options::default(),
    );
    assert_eq!(normal.verdict, Verdict::Secure);

    // An absurdly narrow band must find the same network violated.
    let strict = verify(
        &case,
        &contingency,
        VoltageLimits {
            min: 0.999,
            max: 1.001,
        },
        Options::default(),
    );

    assert!(
        strict.verdict.is_concerning(),
        "a 0.2% band found nothing wrong; the limits are not being applied"
    );
}

/// The outcome that must never read as "nothing found". A contingency the
/// power flow cannot solve is one where the network may have no steady state
/// at all, and that is the strongest thing this analysis can say.
#[test]
fn an_unsolvable_contingency_is_the_most_serious_verdict() {
    let case = healthy();

    // One iteration is not enough to converge from flat, which stands in for a
    // case with no reachable solution — the reporting path is the same.
    let result = verify(
        &case,
        &Contingency::new("s-a", 0),
        VoltageLimits::default(),
        Options {
            tolerance: 1e-12,
            max_iterations: 1,
        },
    );

    assert!(matches!(result.verdict, Verdict::Insolvable(_)));
    assert!(
        result.verdict.is_concerning(),
        "an insolvable contingency read as unconcerning, which inverts its meaning"
    );
    assert!(result.solution.is_none());
}

/// `verify_top` takes a prefix and does not sort, so which contingencies were
/// verified is visible at the call site rather than decided here.
#[test]
fn verify_top_respects_the_ranking_it_was_given() {
    let case = healthy();
    let pre = solve_dc(&case).unwrap();
    let lodf = Lodf::build(&case).unwrap();
    let ranked = rank(screen(&case, &lodf, &pre.flows, &all_branches(&case), 1.0).unwrap());

    let verified = verify_top(
        &case,
        &ranked,
        2,
        VoltageLimits::default(),
        Options::default(),
    );

    assert_eq!(verified.len(), 2);
    for (result, screened) in verified.iter().zip(&ranked) {
        assert_eq!(result.contingency, screened.contingency);
    }

    // Asking for more than exists yields what exists rather than panicking.
    let all = verify_top(
        &case,
        &ranked,
        99,
        VoltageLimits::default(),
        Options::default(),
    );
    assert_eq!(all.len(), ranked.len());
}

#[test]
fn repeated_verifications_are_bit_identical() {
    let case = healthy();
    let contingency = Contingency::new("s-a", 0);
    let limits = VoltageLimits::default();

    let first = verify(&case, &contingency, limits, Options::default());

    for _ in 0..10 {
        assert_eq!(
            first,
            verify(&case, &contingency, limits, Options::default())
        );
    }
}
