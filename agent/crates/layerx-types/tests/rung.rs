use layerx_types::rung::Rung;

const ALL: [Rung; 3] = [Rung::Submitted, Rung::Receipted, Rung::Committed];

#[test]
fn rung_has_exactly_three_variants_with_stable_ranks() {
    let ranks: Vec<u8> = ALL.iter().map(|rung| rung.wire_rank()).collect();
    assert_eq!(ranks, vec![0, 1, 2]);
    for rung in ALL {
        match rung {
            Rung::Submitted | Rung::Receipted | Rung::Committed => {}
        }
    }
}

#[test]
fn rung_orders_submitted_before_receipted_before_committed() {
    assert!(Rung::Submitted < Rung::Receipted);
    assert!(Rung::Receipted < Rung::Committed);
    assert!(Rung::Submitted < Rung::Committed);
    let mut shuffled = vec![Rung::Committed, Rung::Submitted, Rung::Receipted];
    shuffled.sort();
    assert_eq!(shuffled, ALL.to_vec());
    assert_eq!(ALL.iter().max(), Some(&Rung::Committed));
}

#[test]
fn rung_names_round_trip() {
    assert_eq!(Rung::Submitted.name(), "submitted");
    assert_eq!(Rung::Receipted.name(), "receipted");
    assert_eq!(Rung::Committed.name(), "committed");
    for rung in ALL {
        assert_eq!(Rung::from_name(rung.name()), Some(rung));
    }
}

#[test]
fn rung_refuses_retired_and_unknown_names() {
    for retired in [
        "settlement-anchored",
        "checkpoint-finalised",
        "batch-included",
        "state-proven",
        "sequencer-signed",
        "unverified",
        "pending",
        "instant",
        "executed",
        "batched",
        "finalised",
        "sealed",
        "final",
        "Submitted",
        "RECEIPTED",
        " committed",
        "committed ",
        "",
    ] {
        assert_eq!(
            Rung::from_name(retired),
            None,
            "{retired:?} must be refused"
        );
    }
}
