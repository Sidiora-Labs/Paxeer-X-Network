use layerx_agent_api::identity::{
    ActivityType, AgentDid, Asset, AuthorityRef, ContractError, ExplicitSet,
};
use layerx_agent_api::prepare::{
    CanonicalBytes, Disclosure, IdempotencyRef, PreparationRef, Prepared, SigningPreimage,
};
use layerx_agent_api::submit::SignatureBytes;
use layerx_agent_api::track::{
    ReceiptRef, SubmissionRef, SubmissionState, TrackedSubmission, WaitRequest, WaitResult,
};
use layerx_agent_api::{Amount, TimestampSeconds};
use layerx_types::rung::Rung;

const SCHEMA: &str = include_str!("../../../schema/agent-api/write.kvx");
const GOLDEN: &str = include_str!("../../../schema/agent-api/golden/write.kvx");

fn required<T>(result: Result<T, layerx_agent_api::identity::ContractError>) -> T {
    result.unwrap_or_else(|error| panic!("valid contract value: {error:?}"))
}

#[test]
fn preparation_binds_exact_bytes_preimage_disclosure_and_expiry() {
    let digest = [7; 32];
    let prepared = Prepared {
        preparation_ref: required(PreparationRef::new("prep-7")),
        unsigned_canonical_bytes: required(CanonicalBytes::new(vec![1, 2, 3])),
        signing_preimage: required(SigningPreimage::new(vec![4, 5, 6])),
        disclosure: Disclosure {
            canonical_digest: digest,
            activity_type: ActivityType(9),
            actor: required(AgentDid::new("did:layerx:actor")),
            authority: required(AuthorityRef::new("owner-1")),
            counterparties: ExplicitSet::allow(vec![required(AgentDid::new("did:layerx:payee"))]),
            amounts: ExplicitSet::deny_all(),
            asset: required(Asset::new("LXP")),
            fee_limit: Amount(3),
            expiry: TimestampSeconds(100),
            idempotency_key: required(IdempotencyRef::new("idem-7")),
        },
        expiry: TimestampSeconds(100),
    };
    assert_eq!(prepared.disclosure.canonical_digest, digest);
    assert_eq!(prepared.expiry, prepared.disclosure.expiry);
    assert!(SCHEMA.contains("origin = \"decoded_from_unsigned_canonical_bytes\""));
}

#[test]
fn submission_states_major_two() {
    let states = [
        SubmissionState::Prepared,
        SubmissionState::Signed,
        SubmissionState::Queued,
        SubmissionState::Submitted,
        SubmissionState::Unknown,
        SubmissionState::Executed {
            receipt_ref: required(ReceiptRef::new("receipt-1")),
        },
        SubmissionState::Committed {
            receipt_ref: required(ReceiptRef::new("receipt-1")),
        },
        SubmissionState::Failed {
            result: layerx_types::result::KnownResult::BadSignature.into(),
        },
        SubmissionState::Expired,
    ];
    assert_eq!(
        states.iter().map(SubmissionState::name).collect::<Vec<_>>(),
        [
            "prepared",
            "signed",
            "queued",
            "submitted",
            "unknown",
            "executed",
            "committed",
            "failed",
            "expired",
        ]
    );
    assert!(SCHEMA.contains(
        "variants = [\"Prepared\",\"Signed\",\"Queued\",\"Submitted\",\"Unknown\",\"Executed\",\"Committed\",\"Failed\",\"Expired\"]"
    ));
    assert!(SCHEMA.contains("Unknown.rung = \"submitted\""));
    assert!(SCHEMA.contains("Executed.rung = \"receipted\""));
    assert!(SCHEMA.contains("Unknown.semantics = \"first_class_terminal_pending\""));
}

#[test]
fn executed_state_cannot_exist_without_a_receipt_reference() {
    assert_eq!(
        ReceiptRef::new(""),
        Err(ContractError::Empty("receipt_ref"))
    );
    let executed = SubmissionState::Executed {
        receipt_ref: required(ReceiptRef::new("receipt-9")),
    };
    let SubmissionState::Executed { receipt_ref } = executed else {
        panic!("executed variant expected");
    };
    assert_eq!(receipt_ref.as_str(), "receipt-9");
    assert!(SCHEMA.contains("Executed.required = [\"receipt_ref\"]"));
}

#[test]
fn wait_returns_actual_rung_even_when_deadline_elapsed() {
    let submission_ref = required(SubmissionRef::new("submission-1"));
    let request = WaitRequest {
        submission_ref: submission_ref.clone(),
        requested_rung: Rung::Committed,
        deadline: TimestampSeconds(50),
    };
    let tracked = TrackedSubmission {
        submission_ref,
        state: SubmissionState::Unknown,
        evidence: Vec::new(),
        rung: Rung::Submitted,
        transitions: Vec::new(),
    };
    let result = WaitResult {
        submission: tracked,
        actual_rung: Rung::Submitted,
        deadline_elapsed: true,
    };
    assert!(result.deadline_elapsed);
    assert_eq!(result.actual_rung, result.submission.rung);
    assert_eq!(result.actual_rung, Rung::Submitted);
    assert_eq!(request.requested_rung, Rung::Committed);
    assert_eq!(result.submission.state, SubmissionState::Unknown);
    assert!(result.actual_rung < request.requested_rung);
}

#[test]
fn sign_signature_is_65_bytes() {
    let response = GOLDEN
        .split_once("[golden.response.sign]\n")
        .map(|(_, response)| response)
        .unwrap_or_else(|| panic!("golden sign response missing"));
    let signature_text = response
        .lines()
        .take_while(|line| !line.starts_with('['))
        .find_map(|line| {
            line.strip_prefix("signature = \"")
                .and_then(|value| value.strip_suffix('"'))
        })
        .unwrap_or_else(|| panic!("golden sign signature missing"));
    assert_eq!(signature_text.len(), 130);
    let decoded = required(signature_text.parse::<SignatureBytes>());
    let mut expected = vec![1; 32];
    expected.extend_from_slice(&[2; 32]);
    expected.push(0);
    assert_eq!(decoded.as_bytes(), expected);
    assert_eq!(decoded.as_bytes().len(), 65);
    assert_eq!(required(SignatureBytes::new(expected)), decoded);
    assert!(SCHEMA.contains("signature_encoding = \"130 lowercase hex characters, 65 bytes\""));
    for length in [0, 1, 63, 64, 66, 130] {
        assert_eq!(
            SignatureBytes::new(vec![1; length]),
            Err(ContractError::Malformed("signature"))
        );
        assert_eq!(
            "01".repeat(length).parse::<SignatureBytes>(),
            Err(ContractError::Malformed("signature"))
        );
    }
    for malformed in [
        format!("0x{signature_text}"),
        format!(" {signature_text}"),
        format!("{signature_text}\n"),
        format!("A{}", &signature_text[1..]),
        format!("g{}", &signature_text[1..]),
        format!("é{}", &signature_text[2..]),
        signature_text[..129].to_owned(),
        format!("{signature_text}0"),
    ] {
        assert_eq!(
            malformed.parse::<SignatureBytes>(),
            Err(ContractError::Malformed("signature"))
        );
    }
}

#[test]
fn committed_state_requires_a_receipt_reference() {
    assert_eq!(
        ReceiptRef::new(""),
        Err(ContractError::Empty("receipt_ref"))
    );
    let committed = SubmissionState::Committed {
        receipt_ref: required(ReceiptRef::new("receipt-9")),
    };
    let SubmissionState::Committed { receipt_ref } = committed else {
        panic!("committed variant expected");
    };
    assert_eq!(receipt_ref.as_str(), "receipt-9");
    assert!(SCHEMA.contains("Committed.required = [\"receipt_ref\"]"));
    assert!(SCHEMA.contains("Committed.rung = \"committed\""));
    assert!(Rung::Submitted < Rung::Receipted);
    assert!(Rung::Receipted < Rung::Committed);
}
