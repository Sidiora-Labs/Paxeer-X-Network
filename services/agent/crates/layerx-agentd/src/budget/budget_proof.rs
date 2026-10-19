use layerx_client::evidence::{
    verify_account_evidence, verify_module_evidence, AccountEvidencePolicy, RootSelector,
    VerifiedAccountEvidence, VerifiedCheckpoint,
};
use layerx_client::head::Head;
use layerx_programs::hex;
use layerx_types::ids::Did;
use layerx_types::verify::VerificationLevel;
use serde::Deserialize;
use sha2::{Digest, Sha256};
use std::collections::BTreeMap;

pub const MAX_BUDGET_PROOF_BYTES: usize = 1_048_576;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum BudgetProofError {
    Refused,
}

pub struct BudgetProofTrust<'a> {
    pub tenant: &'a str,
    pub principal: &'a str,
    pub owner: &'a Did,
    pub protocol_version: u16,
    pub network_id: u32,
    pub sequencer_key: [u8; 32],
    pub head: Head,
    pub checkpoint: &'a VerifiedCheckpoint,
    pub now_ms: u64,
    pub maximum_age_seconds: u64,
}

pub struct VerifiedBudgetProof {
    budget_id: [u8; 32],
    asset: [u8; 32],
    source_account: [u8; 32],
    owner: String,
    observed_head_sequence: u64,
    remaining: u128,
    verification: VerificationLevel,
    evidence_digest: [u8; 32],
    receipt_digest: [u8; 32],
    checkpoint_digest: [u8; 32],
    age_sequences: u64,
    maximum_age_sequences: u64,
    digest: [u8; 32],
    canonical_export_bytes: Vec<u8>,
}

impl VerifiedBudgetProof {
    pub const fn budget_id(&self) -> [u8; 32] {
        self.budget_id
    }
    pub const fn asset(&self) -> [u8; 32] {
        self.asset
    }
    pub const fn source_account(&self) -> [u8; 32] {
        self.source_account
    }
    pub fn owner(&self) -> &str {
        &self.owner
    }
    pub const fn observed_head_sequence(&self) -> u64 {
        self.observed_head_sequence
    }
    pub const fn remaining(&self) -> u128 {
        self.remaining
    }
    pub const fn verification(&self) -> VerificationLevel {
        self.verification
    }
    pub const fn evidence_digest(&self) -> [u8; 32] {
        self.evidence_digest
    }
    pub const fn receipt_digest(&self) -> [u8; 32] {
        self.receipt_digest
    }
    pub const fn checkpoint_digest(&self) -> [u8; 32] {
        self.checkpoint_digest
    }
    pub const fn age_sequences(&self) -> u64 {
        self.age_sequences
    }
    pub const fn maximum_age_sequences(&self) -> u64 {
        self.maximum_age_sequences
    }
    pub const fn digest(&self) -> [u8; 32] {
        self.digest
    }
    pub fn canonical_export_bytes(&self) -> &[u8] {
        &self.canonical_export_bytes
    }
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Export {
    schema: String,
    tenant: String,
    principal: String,
    protocol_version: u16,
    network_id: u32,
    lni_interface_version: Version,
    maximum_age_seconds: u64,
    head: ExportHead,
    budget_state: State,
    accounts: Accounts,
    identity_key: String,
    proofs: Vec<Row>,
    finality: Finality,
    authorization: Authorization,
    maximum_bytes: usize,
}
#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Version {
    major: u16,
    minor: u16,
}
#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct ExportHead {
    batch_number: u64,
    global_sequence: u64,
    checkpoint_id: String,
}
#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Accounts {
    owner: String,
    budget: String,
    source: String,
}
#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct State {
    budget_id: String,
    asset: String,
    revocation_sequence: u64,
    observed_head_sequence: u64,
    verification: u8,
    evidence_digest: String,
    receipt_digest: String,
    checkpoint_digest: String,
    age_sequences: u64,
    maximum_age_sequences: u64,
    remaining: String,
    closed: bool,
    revoked: bool,
    canonical_core_bytes: String,
}
#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Finality {
    canonical_header: String,
    checkpoint_bytes: String,
    context_bytes: String,
    checkpoint_id: String,
    set_version: u64,
    observed_block_hash: Option<String>,
}
#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Authorization {
    sequencer_id: String,
    public_key: String,
    first_batch_number: u64,
    last_batch_number: u64,
}
#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Row {
    selector: Selector,
    canonical_bytes: String,
    proof_material: String,
    verification: u8,
    batch_number: u64,
    global_sequence: u64,
    observed_checkpoint: String,
}
#[derive(Deserialize)]
#[serde(tag = "kind", deny_unknown_fields)]
enum Selector {
    #[serde(rename = "module")]
    Module { module_id: u16, key: String },
    #[serde(rename = "account")]
    Account { account_id: String },
}

fn require(value: bool) -> Result<(), BudgetProofError> {
    if value {
        Ok(())
    } else {
        Err(BudgetProofError::Refused)
    }
}
fn bytes(value: &str) -> Result<Vec<u8>, BudgetProofError> {
    require(!value.is_empty() && value.len() <= MAX_BUDGET_PROOF_BYTES && value.len() % 2 == 0)?;
    let decoded = hex::decode(value).map_err(|_| BudgetProofError::Refused)?;
    require(hex::encode(&decoded) == value)?;
    Ok(decoded)
}
fn id(value: &str) -> Result<[u8; 32], BudgetProofError> {
    bytes(value)?
        .try_into()
        .map_err(|_| BudgetProofError::Refused)
}

pub fn verify_budget_proof(
    export: &[u8],
    budget_id: [u8; 32],
    trust: &BudgetProofTrust<'_>,
) -> Result<VerifiedBudgetProof, BudgetProofError> {
    require(!export.is_empty() && export.len() <= MAX_BUDGET_PROOF_BYTES && budget_id != [0; 32])?;
    let value: Export = serde_json::from_slice(export).map_err(|_| BudgetProofError::Refused)?;
    require(
        value.schema == "layerx.human.budget-proof.v1"
            && value.tenant == trust.tenant
            && value.principal == trust.principal
            && value.protocol_version == 3
            && value.protocol_version == trust.protocol_version
            && value.network_id == trust.network_id
            && value.lni_interface_version.major == 1
            && value.lni_interface_version.minor == 8
            && value.maximum_bytes == MAX_BUDGET_PROOF_BYTES,
    )?;
    let checkpoint = trust
        .checkpoint
        .report()
        .evidence()
        .checkpoint_id()
        .ok_or(BudgetProofError::Refused)?;
    require(
        checkpoint == trust.head.finalised_checkpoint
            && id(&value.finality.checkpoint_id)? == checkpoint
            && id(&value.head.checkpoint_id)? == checkpoint
            && value.head.batch_number == trust.head.sealed_batch
            && value.head.global_sequence == trust.head.chain_sequence
            && trust.head.chain_sequence != 0,
    )?;
    let header = bytes(&value.finality.canonical_header)?;
    require(
        header == trust.checkpoint.canonical_header()
            && bytes(&value.finality.checkpoint_bytes)? == trust.checkpoint.checkpoint_bytes()
            && bytes(&value.finality.context_bytes)? == trust.checkpoint.context_bytes()
            && value.finality.set_version == trust.checkpoint.set_version()
            && value
                .finality
                .observed_block_hash
                .as_deref()
                .map(id)
                .transpose()?
                == trust.checkpoint.observed_block_hash(),
    )?;
    let decoded = layerx_wire::receipt::decode_batch_header(&header)
        .map_err(|_| BudgetProofError::Refused)?;
    require(
        decoded.last_sequence() == trust.head.chain_sequence
            && decoded.batch_number() == trust.head.sealed_batch
            && trust.maximum_age_seconds > 0
            && value.maximum_age_seconds > 0
            && value.maximum_age_seconds <= trust.maximum_age_seconds,
    )?;
    let age = trust
        .now_ms
        .checked_sub(decoded.timestamp_ms())
        .ok_or(BudgetProofError::Refused)?;
    require(
        age <= trust
            .maximum_age_seconds
            .checked_mul(1000)
            .ok_or(BudgetProofError::Refused)?,
    )?;
    let public_key = id(&value.authorization.public_key)?;
    require(
        public_key == trust.sequencer_key
            && value.authorization.first_batch_number <= trust.head.sealed_batch
            && value.authorization.last_batch_number >= trust.head.sealed_batch,
    )?;
    let policy = AccountEvidencePolicy {
        expected_protocol_version: trust.protocol_version,
        expected_network_id: trust.network_id,
        handshake_sequencer_key: trust.sequencer_key,
        root_selector: RootSelector::Checkpoint(checkpoint),
    };
    require((4..=5).contains(&value.proofs.len()))?;
    let mut accounts: BTreeMap<[u8; 32], VerifiedAccountEvidence> = BTreeMap::new();
    let mut modules = BTreeMap::new();
    let mut evidence = Sha256::new();
    evidence.update(b"layerx-human/native-budget-evidence/v1\0");
    evidence.update(checkpoint);
    let mut level = trust.checkpoint.report().level();
    for row in &value.proofs {
        require(
            row.batch_number == trust.head.sealed_batch
                && row.global_sequence == trust.head.chain_sequence
                && id(&row.observed_checkpoint)? == checkpoint,
        )?;
        let canonical = bytes(&row.canonical_bytes)?;
        let proof = bytes(&row.proof_material)?;
        for material in [&canonical, &proof] {
            evidence.update(
                u64::try_from(material.len())
                    .map_err(|_| BudgetProofError::Refused)?
                    .to_be_bytes(),
            );
            evidence.update(material)
        }
        let (verified_level, signed) = match &row.selector {
            Selector::Account { account_id } => {
                let account_id = id(account_id)?;
                let verified =
                    verify_account_evidence(&canonical, &proof, account_id, None, policy)
                        .map_err(|_| BudgetProofError::Refused)?;
                let level = verified.level();
                let signed = verified.signed_header().clone();
                require(accounts.insert(account_id, verified).is_none())?;
                (level, signed)
            }
            Selector::Module { module_id, key } => {
                let key = bytes(key)?;
                let verified = verify_module_evidence(&canonical, &proof, *module_id, &key, policy)
                    .map_err(|_| BudgetProofError::Refused)?;
                require(modules.insert((*module_id, key), canonical).is_none())?;
                (verified.level(), verified.signed_header().clone())
            }
        };
        require(
            verified_level >= VerificationLevel::CHECKPOINT_FINALISED
                && row.verification == verified_level.wire_rank()
                && signed.canonical_bytes == header
                && signed.public_key == public_key
                && signed.sequencer_id == id(&value.authorization.sequencer_id)?
                && signed.first_batch_number == value.authorization.first_batch_number
                && signed.last_batch_number == value.authorization.last_batch_number,
        )?;
        level = level.min(verified_level);
    }
    let key = super::budget_state_key(budget_id);
    let canonical = modules
        .get(&(3, key.clone()))
        .ok_or(BudgetProofError::Refused)?;
    let record = super::ProtocolBudgetRecord::decode_state(&key, canonical)
        .map_err(|_| BudgetProofError::Refused)?;
    let source = record.source_account.unwrap_or(record.owner);
    require(
        id(&value.accounts.owner)? == record.owner
            && id(&value.accounts.budget)? == record.budget_account
            && id(&value.accounts.source)? == source,
    )?;
    let owner = accounts
        .get(&record.owner)
        .ok_or(BudgetProofError::Refused)?
        .account();
    let owner_did =
        std::str::from_utf8(trust.owner.as_bytes()).map_err(|_| BudgetProofError::Refused)?;
    require(
        owner.name == format!("agent:{owner_did}:main").as_bytes()
            && owner.kind == 1
            && !owner.frozen
            && owner.authority_key.is_some(),
    )?;
    let mut identity_preimage = b"LXP/v1/did-id\0".to_vec();
    identity_preimage.extend(
        u16::try_from(owner_did.len())
            .map_err(|_| BudgetProofError::Refused)?
            .to_be_bytes(),
    );
    identity_preimage.extend(owner_did.as_bytes());
    let identity_key: [u8; 32] = Sha256::digest(identity_preimage).into();
    require(id(&value.identity_key)? == identity_key)?;
    let identity = modules
        .get(&(7, identity_key.to_vec()))
        .ok_or(BudgetProofError::Refused)?;
    require(
        modules.len() == 2
            && identity.len() == 223
            && &identity[..5] == b"LXGI1"
            && identity[5..37] == identity_key
            && owner.authority_key.as_ref().map(<[u8; 32]>::as_slice) == Some(&identity[37..69]),
    )?;
    for offset in [69, 215] {
        let sequence = u64::from_be_bytes(
            identity[offset..offset + 8]
                .try_into()
                .map_err(|_| BudgetProofError::Refused)?,
        );
        require(sequence > 0 && sequence <= trust.head.chain_sequence)?;
    }
    let budget = accounts
        .get(&record.budget_account)
        .ok_or(BudgetProofError::Refused)?;
    require(
        budget.account().name
            == format!("agent:{owner_did}:budget:{}", hex::encode(&budget_id)).as_bytes()
            && budget.account().kind == 2
            && budget.account().asset_id() == record.asset_id,
    )?;
    let source_account = accounts
        .get(&source)
        .ok_or(BudgetProofError::Refused)?
        .account();
    require(
        source_account.kind == 1
            && source_account.asset_id() == record.asset_id
            && source_account.authority_key == owner.authority_key
            && [
                format!("agent:{owner_did}:main"),
                format!("agent:{owner_did}:asset:{}", hex::encode(&record.asset_id)),
            ]
            .iter()
            .any(|name| name.as_bytes() == source_account.name)
            && accounts.len() == if source == record.owner { 2 } else { 3 },
    )?;
    let remaining = if record.closed
        || record.revoked
        || budget.account().frozen
        || decoded.timestamp_ms() >= record.expiry
        || decoded.timestamp_ms() < record.period_start
    {
        0
    } else {
        record.remaining().min(budget.account().balance())
    };
    let digest: [u8; 32] = evidence.finalize().into();
    let state = &value.budget_state;
    require(
        id(&state.budget_id)? == budget_id
            && id(&state.asset)? == record.asset_id
            && state.revocation_sequence == record.revocation_sequence
            && state.revocation_sequence > 0
            && state.revocation_sequence <= trust.head.chain_sequence
            && state.observed_head_sequence == trust.head.chain_sequence
            && state.verification == level.wire_rank()
            && id(&state.evidence_digest)? == digest
            && id(&state.receipt_digest)? == budget.receipt_digest()
            && id(&state.checkpoint_digest)? == checkpoint
            && state.age_sequences == 0
            && state.maximum_age_sequences > 0
            && state.remaining == remaining.to_string()
            && state.closed == record.closed
            && state.revoked == record.revoked
            && bytes(&state.canonical_core_bytes)? == *canonical,
    )?;
    Ok(VerifiedBudgetProof {
        budget_id,
        asset: record.asset_id,
        source_account: source,
        owner: owner_did.to_owned(),
        observed_head_sequence: trust.head.chain_sequence,
        remaining,
        verification: level,
        evidence_digest: digest,
        receipt_digest: budget.receipt_digest(),
        checkpoint_digest: checkpoint,
        age_sequences: state.age_sequences,
        maximum_age_sequences: state.maximum_age_sequences,
        digest: Sha256::digest(export).into(),
        canonical_export_bytes: export.to_vec(),
    })
}
