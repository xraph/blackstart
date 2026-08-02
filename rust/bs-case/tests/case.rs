//! Structural checks on the solvable case.
//!
//! These cover `validate` rather than any numerics: the point of validating
//! before assembly is that a modelling mistake gets named as a modelling
//! mistake, so the naming is the behaviour worth pinning.

use bs_case::{Branch, Bus, Case, CaseError};
use bs_quantity::Base;

fn base() -> Base {
    Base::new(100.0, 138.0).unwrap()
}

fn two_bus() -> Case {
    Case::new(
        "two-bus",
        base(),
        vec![Bus::slack("s", 1.0), Bus::load("l", -0.5, -0.1)],
        vec![Branch::new("line", 0, 1, 0.01, 0.1)],
    )
}

#[test]
fn a_well_formed_case_validates() {
    let case = two_bus();

    assert_eq!(case.validate(), Ok(()));
    assert_eq!(case.slack_index(), Ok(0));
    assert_eq!(case.n_bus(), 2);
    assert_eq!(case.n_branch(), 1);
}

#[test]
fn an_empty_case_is_empty_not_slackless() {
    // Ordering matters: reporting NoSlack for a case with no buses at all
    // sends the reader looking for a missing bus kind rather than a missing
    // case.
    let case = Case::new("nothing", base(), vec![], vec![]);

    assert_eq!(case.validate(), Err(CaseError::Empty));
}

#[test]
fn each_structural_mistake_has_its_own_error() {
    let cases = [
        (
            Case::new(
                "no-slack",
                base(),
                vec![Bus::load("a", -0.5, 0.0), Bus::load("b", 0.5, 0.0)],
                vec![],
            ),
            CaseError::NoSlack,
        ),
        (
            Case::new(
                "three-slack",
                base(),
                vec![
                    Bus::slack("a", 1.0),
                    Bus::slack("b", 1.0),
                    Bus::slack("c", 1.0),
                ],
                vec![],
            ),
            CaseError::MultipleSlack { count: 3 },
        ),
        (
            Case::new(
                "dangling",
                base(),
                vec![Bus::slack("a", 1.0), Bus::load("b", 0.0, 0.0)],
                vec![Branch::new("l", 1, 9, 0.0, 0.1)],
            ),
            CaseError::BranchOutOfRange { branch: 0, bus: 9 },
        ),
        (
            Case::new(
                "self-loop",
                base(),
                vec![Bus::slack("a", 1.0), Bus::load("b", 0.0, 0.0)],
                vec![Branch::new("l", 1, 1, 0.0, 0.1)],
            ),
            CaseError::SelfLoop { branch: 0, bus: 1 },
        ),
        (
            Case::new(
                "zero-x",
                base(),
                vec![Bus::slack("a", 1.0), Bus::load("b", 0.0, 0.0)],
                vec![Branch::new("l", 0, 1, 0.05, 0.0)],
            ),
            CaseError::ZeroReactance { branch: 0 },
        ),
    ];

    for (case, want) in cases {
        assert_eq!(case.validate(), Err(want), "case {}", case.name);
    }
}

/// Every error must say enough to act on without opening the source. A
/// message that only names the variant sends the reader back to the code.
#[test]
fn error_messages_name_the_offending_element() {
    let msg = CaseError::BranchOutOfRange { branch: 4, bus: 91 }.to_string();
    assert!(msg.contains('4') && msg.contains("91"), "{msg}");

    let msg = CaseError::MultipleSlack { count: 3 }.to_string();
    assert!(msg.contains('3'), "{msg}");

    // The two singular-matrix causes must be distinguishable in prose, since
    // that is all an operator reading a log has.
    assert!(CaseError::NoSlack.to_string().contains("no slack"));
    assert!(
        CaseError::MultipleSlack { count: 2 }
            .to_string()
            .contains("over-determined")
    );
}

#[test]
fn out_of_service_branches_are_excluded_but_keep_their_index() {
    let mut case = two_bus();
    case.branches.push(Branch::new("second", 0, 1, 0.01, 0.2));
    case.branches[0].in_service = false;

    assert_eq!(case.live_branches(), vec![1]);
    // The count is of branches, live or not — index space is stable so a
    // contingency can restore one without renumbering.
    assert_eq!(case.n_branch(), 2);
}

#[test]
fn total_injection_is_the_imbalance_the_slack_must_cover() {
    let case = Case::new(
        "unbalanced",
        base(),
        vec![
            Bus::slack("s", 1.0),
            Bus::load("l", -1.5, 0.0),
            Bus::generator("g", 0.4, 1.0),
        ],
        vec![
            Branch::new("a", 0, 1, 0.0, 0.1),
            Branch::new("b", 1, 2, 0.0, 0.1),
        ],
    );

    // -1.5 + 0.4 = -1.1, so the slack supplies 1.1 pu.
    assert!((case.total_injection() + 1.1).abs() < 1e-12);
}
