mod record;

use self::record::Record;
use super::super::{
    CORRELATION, IO_TIMEOUT, LNI_FRAME_BYTES, MAX_LNI_CONNECTIONS, PROTOCOL_VERSION,
};
use super::{digest, hex, json, refusal, unavailable, Config, PrincipalPolicy, Response};
use layerx_client::evidence::{
    verify_account_evidence, verify_account_evidence_with_history, AccountEvidencePolicy,
    RootSelector, VerifiedAccountEvidence, VerifiedCheckpoint,
};
use layerx_client::head::Head;
use layerx_client::lni::handshake::{perform, HandshakeConfig};
use layerx_client::lni::schema::Version;
use layerx_client::lni::transport::{Limits, Uds};
use layerx_client::read::{self, ReadContext, ReadValue, Requested};
use layerx_proof::inclusion::SequencerAuthorization;
use layerx_proof::state::CanonicalAccount;
use layerx_types::verify::VerificationLevel;
use serde_json::json as value;
use sha2::{Digest, Sha256};
use std::sync::atomic::Ordering;
use std::time::{SystemTime, UNIX_EPOCH};

const MAX_EXPORT_BYTES: usize = 1_048_576;
const MAX_EXPORT_MATERIAL_BYTES: usize = (MAX_EXPORT_BYTES - 16_384) / 2;
const MAX_EXPORT_READS: usize = 5;

pub(super) struct Session<'a> {
    config: &'a Config,
    transport: Uds,
    pub(super) context: ReadContext,
    pub(super) checkpoint: VerifiedCheckpoint,
    evidence: Sha256,
    history: Option<layerx_client::handover::SequencerHistory>,
    observed_key: [u8; 32],
    export: Option<Vec<serde_json::Value>>,
    export_material_bytes: usize,
}

fn limits() -> Limits {
    Limits {
        maximum_frame_bytes: LNI_FRAME_BYTES,
        maximum_connections: MAX_LNI_CONNECTIONS,
        maximum_streams: 1,
        maximum_queued_bytes: LNI_FRAME_BYTES,
        deadline: IO_TIMEOUT,
    }
}

fn handshake(config: &Config, interface_version: Version) -> HandshakeConfig {
    HandshakeConfig {
        built_interface_version: interface_version,
        expected_protocol_version: PROTOCOL_VERSION,
        expected_network_id: config.protocol_network_id,
    }
}

fn correlation() -> Result<u64, ()> {
    CORRELATION
        .fetch_update(Ordering::AcqRel, Ordering::Acquire, |value| {
            value.checked_add(1).filter(|_| value != 0)
        })
        .map_err(|_| ())
}

impl<'a> Session<'a> {
    pub(super) fn open(config: &'a Config) -> Result<Self, ()> {
        Self::open_profile(config, Version::V1_5)
    }

    fn open_profile(config: &'a Config, interface_version: Version) -> Result<Self, ()> {
        let deadline = std::time::Instant::now()
            .checked_add(IO_TIMEOUT)
            .ok_or(())?;
        let mut transport =
            Uds::connect(&config.lni_socket, &config.lni_gate, limits()).map_err(|_| ())?;
        let result =
            perform(&mut transport, &handshake(config, interface_version), None).map_err(|_| ())?;
        let node = result.node();
        if node.interface_version != interface_version
            || node.latest_finalised_checkpoint == [0; 32]
        {
            return Err(());
        }
        let head = Head {
            chain_sequence: node.chain_head_sequence,
            sealed_batch: node.latest_sealed_batch,
            finalised_checkpoint: node.latest_finalised_checkpoint,
        };
        let history = crate::trust::snapshot(
            config,
            head.sealed_batch,
            node.authorised_sequencer_key,
            deadline,
        )?;
        let authorization = match &history {
            Some(history) => history
                .authorization_for_batch(head.sealed_batch)
                .map_err(|_| ())?,
            None => config.authorization,
        };
        let checkpoint = crate::trust::checkpoint(
            config,
            &mut transport,
            head.sealed_batch,
            node.interface_version,
            history.as_ref(),
        )?;
        let header = layerx_wire::receipt::decode_batch_header(checkpoint.canonical_header())
            .map_err(|_| ())?;
        if checkpoint.report().evidence().checkpoint_id() != Some(head.finalised_checkpoint)
            || header.last_sequence() != head.chain_sequence
            || header.batch_number() != head.sealed_batch
        {
            return Err(());
        }
        let mut evidence = Sha256::new();
        evidence.update(b"layerx-human/native-budget-evidence/v1\0");
        evidence.update(head.finalised_checkpoint);
        Ok(Self {
            config,
            transport,
            checkpoint,
            evidence,
            history,
            observed_key: node.authorised_sequencer_key,
            export: None,
            export_material_bytes: 0,
            context: ReadContext {
                interface_version: node.interface_version,
                correlation_id: 0,
                expected_protocol_version: PROTOCOL_VERSION,
                expected_network_id: config.protocol_network_id,
                requested: Requested::new(VerificationLevel::CHECKPOINT_FINALISED),
                head,
                sequencer_authorization: authorization,
                handshake_sequencer_key: authorization.public_key(),
                root_selector: RootSelector::Checkpoint(head.finalised_checkpoint),
            },
        })
    }

    fn context(&mut self) -> Result<ReadContext, ()> {
        self.context.correlation_id = correlation()?;
        Ok(self.context)
    }

    fn retain(&mut self, value: &ReadValue, selector: serde_json::Value) -> Result<(), ()> {
        if value.freshness().batch_number != self.context.head.sealed_batch
            || value.freshness().global_sequence != self.context.head.chain_sequence
            || value.freshness().observed_checkpoint != self.context.head.finalised_checkpoint
        {
            return Err(());
        }
        if let Some(export) = &mut self.export {
            let bytes = value
                .canonical_bytes()
                .len()
                .checked_add(value.proof_material().len())
                .ok_or(())?;
            self.export_material_bytes = self.export_material_bytes.checked_add(bytes).ok_or(())?;
            if self.export_material_bytes > MAX_EXPORT_MATERIAL_BYTES
                || export.len() >= MAX_EXPORT_READS
                || value.proof_material().is_empty()
                || value.achieved().wire_rank()
                    < VerificationLevel::CHECKPOINT_FINALISED.wire_rank()
            {
                return Err(());
            }
            export.push(value!({
                "selector": selector,
                "canonical_bytes": hex::encode(value.canonical_bytes()),
                "proof_material": hex::encode(value.proof_material()),
                "verification": value.achieved().wire_rank(),
                "batch_number": value.freshness().batch_number,
                "global_sequence": value.freshness().global_sequence,
                "observed_checkpoint": hex::encode(&value.freshness().observed_checkpoint)
            }));
        }
        for bytes in [value.canonical_bytes(), value.proof_material()] {
            self.evidence
                .update(u64::try_from(bytes.len()).map_err(|_| ())?.to_be_bytes());
            self.evidence.update(bytes);
        }
        Ok(())
    }

    pub(super) fn module(&mut self, module: u16, key: &[u8]) -> Result<Vec<u8>, ()> {
        let context = self.context()?;
        let value = match &self.history {
            Some(history) => {
                read::module_state_with_history(&mut self.transport, module, key, context, history)
            }
            None => read::module_state(&mut self.transport, module, key, context),
        }
        .map_err(|_| ())?;
        self.retain(
            &value,
            value!({"kind":"module","module_id":module,"key":hex::encode(key)}),
        )?;
        Ok(value.canonical_bytes().to_vec())
    }

    pub(super) fn account(&mut self, id: [u8; 32]) -> Result<VerifiedAccountEvidence, ()> {
        let context = self.context()?;
        let value = match &self.history {
            Some(history) => read::account_with_history(&mut self.transport, id, context, history),
            None => read::account(&mut self.transport, id, context),
        }
        .map_err(|_| ())?;
        self.retain(
            &value,
            value!({"kind":"account","account_id":hex::encode(&id)}),
        )?;
        let policy = AccountEvidencePolicy {
            expected_protocol_version: PROTOCOL_VERSION,
            expected_network_id: self.config.protocol_network_id,
            handshake_sequencer_key: self.context.handshake_sequencer_key,
            root_selector: self.context.root_selector,
        };
        let account = match &self.history {
            Some(history) => verify_account_evidence_with_history(
                value.canonical_bytes(),
                value.proof_material(),
                id,
                None,
                policy,
                history,
            ),
            None => verify_account_evidence(
                value.canonical_bytes(),
                value.proof_material(),
                id,
                None,
                policy,
            ),
        }
        .map_err(|_| ())?;
        let header = account.signed_header();
        let authorization = SequencerAuthorization::new(
            header.sequencer_id,
            header.public_key,
            header.first_batch_number,
            header.last_batch_number,
        );
        if header.canonical_bytes != self.checkpoint.canonical_header()
            || (self.history.is_none() && authorization != self.config.authorization)
        {
            return Err(());
        }
        Ok(account)
    }

    pub(super) fn unchanged(&self) -> Result<(), ()> {
        let mut transport = Uds::connect(&self.config.lni_socket, &self.config.lni_gate, limits())
            .map_err(|_| ())?;
        let result = perform(
            &mut transport,
            &handshake(self.config, self.context.interface_version),
            None,
        )
        .map_err(|_| ())?;
        let node = result.node();
        if node.interface_version != self.context.interface_version
            || node.authorised_sequencer_key != self.observed_key
            || node.chain_head_sequence != self.context.head.chain_sequence
            || node.latest_sealed_batch != self.context.head.sealed_batch
            || node.latest_finalised_checkpoint != self.context.head.finalised_checkpoint
        {
            return Err(());
        }
        Ok(())
    }
}

fn owner_did<'a>(owner: &'a CanonicalAccount, p: &PrincipalPolicy) -> Result<&'a str, ()> {
    let name = std::str::from_utf8(&owner.name).map_err(|_| ())?;
    let did = name
        .strip_prefix("agent:")
        .and_then(|value| value.strip_suffix(":main"))
        .filter(|did| !did.is_empty() && did.len() <= 255)
        .ok_or(())?;
    if owner.kind != 1
        || owner.frozen
        || owner.authority_key.is_none()
        || !p
            .identities
            .iter()
            .any(|identity| identity.did == did && !identity.frozen)
    {
        return Err(());
    }
    Ok(did)
}

pub(super) fn identity_key(did: &str) -> Result<[u8; 32], ()> {
    let mut preimage = b"LXP/v1/did-id\0".to_vec();
    preimage.extend_from_slice(&u16::try_from(did.len()).map_err(|_| ())?.to_be_bytes());
    preimage.extend_from_slice(did.as_bytes());
    Ok(digest(&preimage))
}

fn identity(session: &mut Session<'_>, did: &str, owner: &CanonicalAccount) -> Result<(), ()> {
    let key = identity_key(did)?;
    let bytes = session.module(7, &key)?;
    if bytes.len() != 223
        || &bytes[..5] != b"LXGI1"
        || bytes[5..37] != key
        || owner.authority_key.as_ref().map(<[u8; 32]>::as_slice) != Some(&bytes[37..69])
    {
        return Err(());
    }
    for offset in [69, 215] {
        let sequence = u64::from_be_bytes(bytes[offset..offset + 8].try_into().map_err(|_| ())?);
        if sequence == 0 || sequence > session.context.head.chain_sequence {
            return Err(());
        }
    }
    Ok(())
}

fn accounts(
    session: &mut Session<'_>,
    record: &Record,
    p: &PrincipalPolicy,
) -> Result<(VerifiedAccountEvidence, [u8; 32]), ()> {
    let owner = session.account(record.owner)?;
    let did = owner_did(owner.account(), p)?;
    identity(session, did, owner.account())?;
    let budget = session.account(record.account)?;
    if budget.account().name != format!("agent:{did}:budget:{}", hex::encode(&record.id)).as_bytes()
        || budget.account().kind != 2
        || budget.account().asset_id() != record.asset
    {
        return Err(());
    }
    let source = match record.source {
        Some(source) if source != record.owner => session.account(source)?,
        _ => owner.clone(),
    };
    let source_name = std::str::from_utf8(&source.account().name).map_err(|_| ())?;
    if source.account().kind != 1
        || source.account().asset_id() != record.asset
        || source.account().authority_key != owner.account().authority_key
        || ![
            format!("agent:{did}:main"),
            format!("agent:{did}:asset:{}", hex::encode(&record.asset)),
        ]
        .iter()
        .any(|name| name == source_name)
    {
        return Err(());
    }
    Ok((budget, identity_key(did)?))
}

fn produce(
    config: &Config,
    p: &PrincipalPolicy,
    id: [u8; 32],
    export: bool,
) -> Result<Response, ()> {
    let mut session = if export {
        Session::open_profile(config, Version::V1_8)?
    } else {
        Session::open(config)?
    };
    if export {
        session.export = Some(Vec::new());
    }
    let mut key = b"budget:".to_vec();
    key.extend_from_slice(&id);
    let bytes = session.module(3, &key)?;
    let record = Record::decode(&bytes)?;
    if record.id != id
        || hex::encode(&record.asset) != p.asset_id
        || record.revocation == 0
        || record.revocation > session.context.head.chain_sequence
    {
        return Err(());
    }
    let (budget, identity) = accounts(&mut session, &record, p)?;
    let header = layerx_wire::receipt::decode_batch_header(session.checkpoint.canonical_header())
        .map_err(|_| ())?;
    let timestamp = header.timestamp_ms();
    let now = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map_err(|_| ())?
        .as_millis();
    let age = now.checked_sub(u128::from(timestamp)).ok_or(())?;
    if age > u128::from(p.maximum_age_seconds) * 1000 || p.maximum_age_sequences == 0 {
        return Err(());
    }
    session.unchanged()?;
    let evidence: [u8; 32] = session.evidence.clone().finalize().into();
    let state = value!({
        "budget_id": hex::encode(&record.id), "asset": hex::encode(&record.asset),
        "revocation_sequence": record.revocation,
        "observed_head_sequence": session.context.head.chain_sequence,
        "verification": 4, "evidence_digest": hex::encode(&evidence),
        "receipt_digest": hex::encode(&budget.receipt_digest()),
        "checkpoint_digest": hex::encode(&session.context.head.finalised_checkpoint),
        "age_sequences": 0, "maximum_age_sequences": p.maximum_age_sequences,
        "remaining": record.remaining(budget.account().balance(), timestamp, budget.account().frozen).to_string(),
        "closed": record.closed, "revoked": record.revoked,
        "canonical_core_bytes": hex::encode(&bytes)
    });
    if !export {
        return Ok(json(200, &state));
    }
    let finality_bytes = session
        .checkpoint
        .canonical_header()
        .len()
        .checked_add(session.checkpoint.checkpoint_bytes().len())
        .ok_or(())?
        .checked_add(session.checkpoint.context_bytes().len())
        .ok_or(())?;
    if session
        .export_material_bytes
        .checked_add(finality_bytes)
        .ok_or(())?
        > MAX_EXPORT_MATERIAL_BYTES
    {
        return Err(());
    }
    let authorization = session.context.sequencer_authorization;
    let response = json(
        200,
        &value!({
            "schema": "layerx.human.budget-proof.v1",
            "tenant": p.tenant, "principal": p.principal,
            "protocol_version": PROTOCOL_VERSION, "network_id": config.protocol_network_id,
            "lni_interface_version": {"major":session.context.interface_version.major,"minor":session.context.interface_version.minor},
            "maximum_age_seconds":p.maximum_age_seconds,
            "head": {"batch_number":session.context.head.sealed_batch,"global_sequence":session.context.head.chain_sequence,"checkpoint_id":hex::encode(&session.context.head.finalised_checkpoint)},
            "budget_state": state,
            "accounts": {"owner":hex::encode(&record.owner),"budget":hex::encode(&record.account),"source":hex::encode(&record.source.unwrap_or(record.owner))},
            "identity_key": hex::encode(&identity),
            "proofs": session.export.as_ref().ok_or(())?,
            "finality": {
                "canonical_header": hex::encode(session.checkpoint.canonical_header()),
                "checkpoint_bytes": hex::encode(session.checkpoint.checkpoint_bytes()),
                "context_bytes": hex::encode(session.checkpoint.context_bytes()),
                "checkpoint_id": hex::encode(&session.context.head.finalised_checkpoint),
                "set_version": session.checkpoint.set_version(),
                "observed_block_hash": session.checkpoint.observed_block_hash().map(|hash| hex::encode(&hash))
            },
            "authorization": {"sequencer_id":hex::encode(&authorization.sequencer_id()),"public_key":hex::encode(&authorization.public_key()),"first_batch_number":authorization.first_batch_number(),"last_batch_number":authorization.last_batch_number()},
            "maximum_bytes":MAX_EXPORT_BYTES
        }),
    );
    if response.body.len() > MAX_EXPORT_BYTES {
        return Err(());
    }
    Ok(response)
}

pub(super) fn read(config: &Config, p: &PrincipalPolicy, id: &str) -> Result<Response, Response> {
    if !p.budgets.iter().any(|bound| bound == id) {
        return Err(refusal(404, "budget_not_bound", None));
    }
    let id = hex::decode32(id).map_err(|_| refusal(400, "invalid_budget_id", None))?;
    produce(config, p, id, false).map_err(|()| unavailable("budget_state_proof_unavailable"))
}

pub(super) fn read_dynamic(
    config: &Config,
    p: &PrincipalPolicy,
    id: &str,
) -> Result<Response, Response> {
    let id = hex::decode32(id).map_err(|_| refusal(400, "invalid_budget_id", None))?;
    produce(config, p, id, false).map_err(|()| unavailable("budget_state_proof_unavailable"))
}

pub(super) fn export(config: &Config, p: &PrincipalPolicy, id: &str) -> Result<Response, Response> {
    if !p.budgets.iter().any(|bound| bound == id) {
        return Err(refusal(404, "budget_not_bound", None));
    }
    export_dynamic(config, p, id)
}

pub(super) fn export_dynamic(
    config: &Config,
    p: &PrincipalPolicy,
    id: &str,
) -> Result<Response, Response> {
    let id = hex::decode32(id).map_err(|_| refusal(400, "invalid_budget_id", None))?;
    produce(config, p, id, true).map_err(|()| unavailable("budget_proof_export_unavailable"))
}
