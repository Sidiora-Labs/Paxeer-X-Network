use layerx_agent_api::export::{
    check_export_response, validate_export_request, FactRef, FactRefError, OfflineExport,
    MAX_FACT_REFS,
};
use layerx_agent_api::identity::ContractError;
use layerx_agent_api::prepare::CanonicalBytes;
use layerx_agent_api::read::{BatchRef, CheckpointRef, Freshness, ReadRequest, RelativeTo, VerifiedRead};
use layerx_agent_api::verify::Level;
use layerx_agent_api::Sequence;
use layerx_agentd::agent_rpc::MAX_BODY_BYTES;
use layerx_agentd::export::{require_frame_within_bound, ExportProduceError, ExportTrustSource, MAX_EXPORT_BYTES};
use layerx_proof::export::OfflineTrustError;
use layerx_proof::settlement::declared_domain;
use layerx_proof::signed_authority::SignedAuthorityHistory;
use layerx_wire::activity::decode_signed;
use layerx_wire::handover::decode_genesis_trust;

const GENESIS: &[u8] = include_bytes!("../../layerx-proof/tests/fixtures/signed-authority/genesis.bin");
const HEADERS: &[u8] = include_bytes!("../../layerx-proof/tests/fixtures/signed-authority/headers.bin");
const HANDOVER: &[u8] =
    include_bytes!("../../layerx-proof/tests/fixtures/signed-authority/handover.activity");

fn must<T, E: std::fmt::Debug>(value: Result<T, E>) -> T {
    value.unwrap_or_else(|error| panic!("daemon export: {error:?}"))
}

fn hex(bytes: &[u8]) -> String {
    bytes.iter().map(|byte| format!("{byte:02x}")).collect()
}

fn fact(value: u8) -> String {
    let mut id = [value; 32];
    id[0] = 0xa0 | (value & 0x0f);
    format!("receipt:{}", hex(&id))
}

fn request(facts: &[String], level: Level) -> ReadRequest<Vec<FactRef>> {
    ReadRequest {
        selector: facts.iter().map(|text| must(FactRef::new(text.clone()))).collect(),
        requested_verification_level: level,
    }
}

fn history() -> (SignedAuthorityHistory, layerx_types::payload::ModuleRegistry) {
    let genesis = must(decode_genesis_trust(GENESIS));
    let activity = must(decode_signed(HANDOVER, &genesis.registry));
    let mut history = must(SignedAuthorityHistory::from_genesis(
        genesis.network_id,
        genesis.canonical_state_root,
        genesis.initial_sequencer_key,
        genesis.governance_witness,
    ));
    for (index, record) in HEADERS.chunks_exact(418).enumerate() {
        let packet = (index >= 13).then_some(activity.payload());
        must(history.advance(&record[..354], &must(record[354..].try_into()), packet));
    }
    (history, genesis.registry)
}

fn response(facts: &[String], level: Level) -> VerifiedRead<OfflineExport> {
    VerifiedRead::new(
        OfflineExport {
            facts: facts.iter().map(|text| must(FactRef::new(text.clone()))).collect(),
            receipts: vec![must(CanonicalBytes::new(b"LXRF".to_vec()))],
            proofs: Vec::new(),
            certificates: Vec::new(),
            headers: Vec::new(),
        },
        level,
        Freshness {
            chain_head: Sequence(9),
            latest_sealed_batch: must(BatchRef::new("1")),
            latest_finalised_checkpoint: must(CheckpointRef::new("1")),
            value_sequence: Sequence(9),
            relative_to: RelativeTo::Batch(must(BatchRef::new("1"))),
        },
    )
}

#[test]
fn export_frame_is_bounded_at_exactly_one_mebibyte() {
    assert_eq!(MAX_EXPORT_BYTES, 1_048_576);
    assert_eq!(MAX_EXPORT_BYTES, MAX_BODY_BYTES);
    assert!(require_frame_within_bound(0).is_ok());
    assert!(require_frame_within_bound(MAX_EXPORT_BYTES).is_ok());
    assert!(matches!(
        require_frame_within_bound(MAX_EXPORT_BYTES + 1),
        Err(ExportProduceError::Oversize)
    ));
    assert!(matches!(
        require_frame_within_bound(usize::MAX),
        Err(ExportProduceError::Oversize)
    ));
}

#[test]
fn export_request_uses_the_shared_strict_grammar_and_keeps_the_requested_level() {
    let facts: Vec<String> = (1..=16).map(fact).collect();
    for level in [Level::SequencerSigned, Level::StateProven, Level::CheckpointFinalised] {
        let validated = must(validate_export_request(request(&facts, level)));
        assert_eq!(validated.requested_verification_level, level);
        assert_eq!(validated.selector, request(&facts, level).selector);
    }
    assert_eq!(MAX_FACT_REFS, 16);
    let seventeen: Vec<String> = (1..=17).map(fact).collect();
    assert_eq!(
        validate_export_request(request(&seventeen, Level::BatchIncluded)).err(),
        Some(FactRefError::TooMany { count: 17 })
    );
    let duplicate = [fact(1), fact(1)];
    assert_eq!(
        validate_export_request(request(&duplicate, Level::BatchIncluded)).err(),
        Some(FactRefError::Duplicate { index: 1 })
    );
    for text in [
        fact(1).to_uppercase(),
        format!("receipt:0x{}", &fact(1)[10..]),
        format!("{} ", fact(1)),
        format!("receipt:{}", hex(&[0; 32])),
        format!("receipts:{}", &fact(1)[8..]),
    ] {
        assert!(
            validate_export_request(request(&[text.clone()], Level::BatchIncluded)).is_err(),
            "accepted {text:?}"
        );
    }
}

#[test]
fn export_response_must_answer_every_requested_fact_at_the_requested_level() {
    let facts = [fact(1), fact(2)];
    let asked = request(&facts, Level::StateProven);
    assert!(check_export_response(&asked, &response(&facts, Level::StateProven)).is_ok());
    assert!(check_export_response(&asked, &response(&facts, Level::CheckpointFinalised)).is_ok());
    // No downgrade: a lower achieved level is refused, not served.
    assert_eq!(
        check_export_response(&asked, &response(&facts, Level::BatchIncluded)).err(),
        Some(ContractError::OutOfRange("export_verification_level"))
    );
    let reordered = [fact(2), fact(1)];
    assert_eq!(
        check_export_response(&asked, &response(&reordered, Level::StateProven)).err(),
        Some(ContractError::Mismatch("export_facts"))
    );
    assert_eq!(
        check_export_response(&asked, &response(&facts[..1], Level::StateProven)).err(),
        Some(ContractError::Mismatch("export_facts"))
    );
}

#[test]
fn export_trust_is_independent_of_the_artifact_and_bound_to_the_authority_network() {
    let (history, registry) = history();
    assert!(declared_domain("undeclared").is_err());
    let domain = must(declared_domain("vectors"));
    let network_matches = domain.network_id() == history.network_id();
    let settlement = domain.settlement();
    match ExportTrustSource::new(history, domain, 1) {
        Ok(source) => {
            assert!(network_matches);
            let trust = must(source.trust(registry));
            assert_eq!(trust.settlement(), settlement);
        }
        Err(error) => {
            if network_matches {
                assert!(!matches!(error, OfflineTrustError::NetworkId));
            } else {
                assert!(matches!(error, OfflineTrustError::NetworkId));
            }
        }
    }
}
