use std::collections::BTreeMap;
use std::rc::Rc;

use layerx_client::evidence::VerifiedAdmissionPrestate;
use layerx_program_sdk::arbiter::{MarketBillingCommitment, MarketSandboxProfile};
use layerx_program_sdk::AccountId;
use layerx_programs_market::{arbitration, attest, decode_lease, decode_offer, decode_usage_claim};
use layerx_programs_runtime::execute::observe_market_sandbox_step;
use layerx_programs_runtime::portable_replay::{
    replay_leaf_hash, replay_node_hash, PortableBoundary,
};
use layerx_programs_runtime::replay::{market_sandbox_baseline_root, MarketSandboxReplayAuthority};
use layerx_programs_runtime::{
    AbiRevision, ArbitrationStepCommitment, FeeSchedule, FeeScheduleParameters, PrincipalId,
    ProgramId, ProgramReplayProfile, ProgramResolver, ResourceBudget, Storage, StorageNamespace,
};
use layerx_proof::inclusion::VerifiedBatchHeader;
use layerx_proof::merkle::Proof;
use layerx_proof::receipt::VerifiedReceipt;
use layerx_proof::state_witness::StateWitness;
use sha2::{Digest, Sha256};

use crate::{AuthenticatedCatalogue, BoundaryProof, VerifiedReplayAuthority};

const MAX_BYTES: usize = 1_048_576;
const MAX_NAMESPACE_VALUES_BYTES: usize = 64 * MAX_BYTES;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum MarketEvidenceError {
    Bounds,
    Encoding,
    Receipt,
    Admission,
    Profile,
    Billing,
    Storage,
    Catalogue,
    Code,
    AttestedInputs,
    Baseline,
    Path,
    Identity,
    Runtime,
}
impl std::fmt::Display for MarketEvidenceError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{self:?}")
    }
}
impl std::error::Error for MarketEvidenceError {}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum MarketStepVerdict {
    Correct,
    Incorrect,
    InvalidEvidence(MarketEvidenceError),
}

pub struct NativeNamespaceProof<'a> {
    pub head: &'a StateWitness,
    pub manifest: &'a [u8],
    pub value_blobs: &'a BTreeMap<[u8; 32], Vec<u8>>,
}
impl NativeNamespaceProof<'_> {
    fn verify(
        &self,
        root: [u8; 32],
        program: [u8; 32],
    ) -> Result<BTreeMap<Vec<u8>, Vec<u8>>, MarketEvidenceError> {
        let mut namespace = program.to_vec();
        namespace.push(1);
        self.verify_namespace(root, &namespace)
    }
    fn verify_namespace(
        &self,
        root: [u8; 32],
        namespace: &[u8],
    ) -> Result<BTreeMap<Vec<u8>, Vec<u8>>, MarketEvidenceError> {
        let mut key = b"progstor".to_vec();
        key.extend_from_slice(namespace);
        if self.head.module_id != 9
            || self.head.key != key
            || self.head.value.len() != 38
            || self.head.verify(root).is_err()
            || self.manifest.len() > MAX_BYTES
        {
            return Err(MarketEvidenceError::Storage);
        }
        let mut head = Reader(&self.head.value);
        if head.u16()? != 1 {
            return Err(MarketEvidenceError::Storage);
        }
        let count = head.u32()?;
        let committed: [u8; 32] = head.array()?;
        if count == 0 || count > 65_536 || digest(self.manifest) != committed {
            return Err(MarketEvidenceError::Storage);
        }
        let mut manifest = Reader(self.manifest);
        if manifest.u16()? != 1 || manifest.u32()? != count {
            return Err(MarketEvidenceError::Storage);
        }
        let mut records = BTreeMap::new();
        let mut total = 0usize;
        for _ in 0..count {
            let length = usize::from(manifest.u16()?);
            if length == 0 || length > 1024 {
                return Err(MarketEvidenceError::Bounds);
            }
            let key = manifest.take(length)?.to_vec();
            if records
                .last_key_value()
                .is_some_and(|(previous, _)| previous >= &key)
            {
                return Err(MarketEvidenceError::Storage);
            }
            let expected_hash: [u8; 32] = manifest.array()?;
            let value_length = manifest.u32()? as usize;
            if value_length > MAX_BYTES {
                return Err(MarketEvidenceError::Bounds);
            }
            let bytes: &[u8] = if value_length == 0 {
                &[]
            } else {
                self.value_blobs
                    .get(&expected_hash)
                    .ok_or(MarketEvidenceError::Storage)?
            };
            if bytes.len() != value_length || digest(bytes) != expected_hash {
                return Err(MarketEvidenceError::Storage);
            }
            total = total
                .checked_add(bytes.len())
                .filter(|n| *n <= MAX_NAMESPACE_VALUES_BYTES)
                .ok_or(MarketEvidenceError::Bounds)?;
            records.insert(key, bytes.to_vec());
        }
        if !manifest.done() {
            return Err(MarketEvidenceError::Storage);
        }
        Ok(records)
    }
}

pub struct MarketReplayEvidence<'a> {
    pub expected_market_program: [u8; 32],
    pub tenant_receipt: &'a VerifiedReceipt,
    pub tenant_admission: &'a VerifiedAdmissionPrestate,
    pub tenant_header: &'a VerifiedBatchHeader,
    pub tenant_receipt_proof: &'a Proof,
    pub provider_receipt: &'a VerifiedReceipt,
    pub provider_admission: &'a VerifiedAdmissionPrestate,
    pub provider_header: &'a VerifiedBatchHeader,
    pub provider_receipt_proof: &'a Proof,
    pub tenant_state: NativeNamespaceProof<'a>,
    pub provider_state: NativeNamespaceProof<'a>,
    pub code_blobs: &'a BTreeMap<[u8; 32], Vec<u8>>,
    pub baseline_storage_bytes: &'a [u8],
    pub baseline_state: Option<NativeNamespaceProof<'a>>,
    pub input_bytes: &'a [u8],
    pub initial_boundary: &'a BoundaryProof,
    pub final_boundary: &'a BoundaryProof,
}

pub struct VerifiedMarketSandbox {
    profile: MarketSandboxProfile,
    billing: MarketBillingCommitment,
    catalogue: Rc<AuthenticatedCatalogue>,
    authority: MarketSandboxReplayAuthority,
}
impl VerifiedMarketSandbox {
    pub fn verify(evidence: MarketReplayEvidence<'_>) -> Result<Self, MarketEvidenceError> {
        let tenant = evidence
            .tenant_receipt
            .receipt()
            .protocol()
            .ok_or(MarketEvidenceError::Receipt)?;
        let provider = evidence
            .provider_receipt
            .receipt()
            .protocol()
            .ok_or(MarketEvidenceError::Receipt)?;
        check_receipt(evidence.tenant_receipt, evidence.tenant_admission)?;
        check_receipt(evidence.provider_receipt, evidence.provider_admission)?;
        if tenant.global_sequence() >= provider.global_sequence()
            || evidence.tenant_admission.network_id() != evidence.provider_admission.network_id()
            || evidence.expected_market_program == [0; 32]
        {
            return Err(MarketEvidenceError::Admission);
        }
        let tenant_authority = VerifiedReplayAuthority::derive_program_call(
            evidence.tenant_receipt,
            evidence.tenant_admission,
            evidence.tenant_header,
            evidence.tenant_receipt_proof,
        )
        .map_err(|_| MarketEvidenceError::Admission)?;
        let provider_authority = VerifiedReplayAuthority::derive_program_call(
            evidence.provider_receipt,
            evidence.provider_admission,
            evidence.provider_header,
            evidence.provider_receipt_proof,
        )
        .map_err(|_| MarketEvidenceError::Admission)?;
        let tenant_records = evidence.tenant_state.verify(
            tenant.resulting_state_root(),
            evidence.expected_market_program,
        )?;
        let provider_records = evidence.provider_state.verify(
            provider.resulting_state_root(),
            evidence.expected_market_program,
        )?;
        let tenant_call =
            market_calldata(evidence.tenant_admission, evidence.expected_market_program)?;
        if tenant_call.get(..2) != Some(&[1, arbitration::AUTHORIZE_SANDBOX_PROFILE]) {
            return Err(MarketEvidenceError::Profile);
        }
        let profile = MarketSandboxProfile::decode(&tenant_call[2..])
            .map_err(|_| MarketEvidenceError::Profile)?;
        let profile_bytes = cell(
            &tenant_records,
            arbitration::PROFILE_PREFIX,
            profile.lease_id,
        )?;
        if profile_bytes != &tenant_call[2..]
            || profile.market_program != evidence.expected_market_program
            || profile.network_id != evidence.tenant_admission.network_id()
            || profile.tenant != tenant_authority.principal()
        {
            return Err(MarketEvidenceError::Profile);
        }
        if cell(
            &provider_records,
            arbitration::PROFILE_PREFIX,
            profile.lease_id,
        )? != profile_bytes
        {
            return Err(MarketEvidenceError::Profile);
        }
        let provider_call = market_calldata(
            evidence.provider_admission,
            evidence.expected_market_program,
        )?;
        if provider_call.len() < 34
            || provider_call[..2] != [1, arbitration::COMMIT_SANDBOX_BILLING]
            || provider_call[2..34] != profile.lease_id
            || provider_authority.principal() != profile.provider
        {
            return Err(MarketEvidenceError::Billing);
        }
        let billing_bytes = cell(
            &provider_records,
            arbitration::BILLING_PREFIX,
            profile.lease_id,
        )?;
        if billing_bytes != &provider_call[34..] {
            return Err(MarketEvidenceError::Billing);
        }
        let billing = MarketBillingCommitment::decode(billing_bytes)
            .map_err(|_| MarketEvidenceError::Billing)?;
        if billing.profile_digest != digest(profile_bytes)
            || billing.boundary_count > profile.maximum_boundaries
        {
            return Err(MarketEvidenceError::Billing);
        }
        let offer = decode_offer(cell(
            &tenant_records,
            b"lx.market.offer/",
            profile.offer_id,
        )?)
        .map_err(|_| MarketEvidenceError::Profile)?;
        let lease = decode_lease(cell(
            &tenant_records,
            b"lx.market.lease/",
            profile.lease_id,
        )?)
        .map_err(|_| MarketEvidenceError::Profile)?;
        let policy = attest::decode_policy(cell(
            &tenant_records,
            b"lx.market.attesters/",
            profile.lease_id,
        )?)
        .map_err(|_| MarketEvidenceError::AttestedInputs)?;
        let sealed = policy
            .settlement_input_commitment()
            .map_err(|_| MarketEvidenceError::AttestedInputs)?;
        if policy.lease_id != profile.lease_id
            || policy.tenant.bytes() != profile.tenant
            || cell(&provider_records, b"lx.market.attesters/", profile.lease_id)?
                != cell(&tenant_records, b"lx.market.attesters/", profile.lease_id)?
        {
            return Err(MarketEvidenceError::AttestedInputs);
        }
        let mut namespace = Sha256::new();
        namespace.update(arbitration::NAMESPACE_DOMAIN);
        namespace.update(profile.sandbox_program);
        namespace.update(profile.lease_id);
        let namespace: [u8; 32] = namespace.finalize().into();
        arbitration::authorize_profile(
            &offer,
            &lease,
            profile,
            AccountId::new(profile.tenant).map_err(|_| MarketEvidenceError::Profile)?,
            evidence.expected_market_program,
            sealed,
            namespace,
            evidence.tenant_header.header().batch_number(),
        )
        .map_err(|_| MarketEvidenceError::Profile)?;
        let provider_offer = decode_offer(cell(
            &provider_records,
            b"lx.market.offer/",
            profile.offer_id,
        )?)
        .map_err(|_| MarketEvidenceError::Billing)?;
        let provider_lease = decode_lease(cell(
            &provider_records,
            b"lx.market.lease/",
            profile.lease_id,
        )?)
        .map_err(|_| MarketEvidenceError::Billing)?;
        let claim = decode_usage_claim(cell(
            &provider_records,
            b"lx.market.claim/",
            profile.lease_id,
        )?)
        .map_err(|_| MarketEvidenceError::Billing)?;
        let (expected_claim, _) = arbitration::billing_claim(
            provider_offer,
            &provider_lease,
            &profile,
            &billing,
            billing.profile_digest,
            AccountId::new(profile.provider).map_err(|_| MarketEvidenceError::Billing)?,
            evidence.provider_header.header().batch_number(),
        )
        .map_err(|_| MarketEvidenceError::Billing)?;
        if claim != expected_claim {
            return Err(MarketEvidenceError::Billing);
        }
        arbitration::matches_claim(&profile, &billing, &claim)
            .map_err(|_| MarketEvidenceError::Billing)?;
        if evidence.baseline_storage_bytes.len() > profile.maximum_bytes as usize
            || evidence.input_bytes.len() > profile.maximum_bytes as usize
        {
            return Err(MarketEvidenceError::Bounds);
        }
        let baseline_storage = Storage::decode_untrusted_replay_state(
            evidence.baseline_storage_bytes,
            profile.maximum_bytes as usize,
        )
        .map_err(|_| MarketEvidenceError::Baseline)?;
        if market_sandbox_baseline_root(&baseline_storage)
            .map_err(|_| MarketEvidenceError::Baseline)?
            != profile.baseline_state_root
        {
            return Err(MarketEvidenceError::Baseline);
        }
        let program =
            ProgramId::new(profile.sandbox_program).map_err(|_| MarketEvidenceError::Code)?;
        let scope =
            PrincipalId::new(profile.namespace).map_err(|_| MarketEvidenceError::Baseline)?;
        let storage_namespace = StorageNamespace::principal(program, scope);
        let namespace_bytes = storage_namespace.canonical_bytes();
        let mut baseline_key = b"progstor".to_vec();
        baseline_key.extend_from_slice(&namespace_bytes);
        let baseline_records = evidence
            .tenant_admission
            .legacy()
            .legacy()
            .program_records();
        let committed_cells = match (
            baseline_records.get(&baseline_key),
            evidence.baseline_state.as_ref(),
        ) {
            (None, None) => BTreeMap::new(),
            (Some(head), Some(proof)) if head == &proof.head.value => {
                proof.verify_namespace(evidence.tenant_admission.state_root(), &namespace_bytes)?
            }
            _ => return Err(MarketEvidenceError::Baseline),
        };
        let inspected = baseline_storage.clone();
        let actual_cells: BTreeMap<Vec<u8>, Vec<u8>> = inspected
            .protocol_namespace_entries(storage_namespace)
            .map_err(|_| MarketEvidenceError::Baseline)?
            .into_iter()
            .collect();
        if actual_cells != committed_cells {
            return Err(MarketEvidenceError::Baseline);
        }
        for (namespace, size) in baseline_storage
            .namespace_sizes()
            .map_err(|_| MarketEvidenceError::Baseline)?
        {
            if namespace != StorageNamespace::principal(program, scope)
                || size > profile.limits.namespace_bytes
            {
                return Err(MarketEvidenceError::Baseline);
            }
        }
        let entrypoint = profile
            .entrypoint()
            .map_err(|_| MarketEvidenceError::Profile)?;
        let mut input = Sha256::new();
        input.update(b"LXP/program-trace-input/v1\0");
        input.update((entrypoint.len() as u32).to_be_bytes());
        input.update(entrypoint.as_bytes());
        input.update((evidence.input_bytes.len() as u64).to_be_bytes());
        input.update(evidence.input_bytes);
        if <[u8; 32]>::from(input.finalize()) != profile.input_digest {
            return Err(MarketEvidenceError::Profile);
        }
        let catalogue =
            AuthenticatedCatalogue::verify(evidence.tenant_admission, evidence.code_blobs)
                .map_err(|_| MarketEvidenceError::Catalogue)?;
        let module = catalogue
            .program_module(program)
            .ok_or(MarketEvidenceError::Code)?;
        let module_abi = match module.abi_revision() {
            AbiRevision::V1 => 1,
            AbiRevision::V2 => 2,
            AbiRevision::V3 => 3,
            AbiRevision::V4 => 4,
        };
        if module.code_hash() != profile.code_hash
            || module_abi != profile.abi_version
            || module.metering_schedule_version() != profile.metering_schedule_version
        {
            return Err(MarketEvidenceError::Code);
        }
        let fees = selected_fees(
            evidence.tenant_admission,
            profile.fee_schedule_version,
            evidence.tenant_header.header().batch_number(),
        )?;
        let limits = profile.limits;
        let authority = MarketSandboxReplayAuthority {
            profile_binding: billing.profile_digest,
            namespace: profile.namespace,
            lease_id: profile.lease_id,
            namespace_limit: limits.namespace_bytes,
            program,
            tenant: PrincipalId::new(profile.tenant).map_err(|_| MarketEvidenceError::Profile)?,
            payment_account: tenant_authority.payer(),
            code_hash: profile.code_hash,
            input_digest: profile.input_digest,
            runtime_version: profile.runtime_version,
            abi_version: profile.abi_version,
            fee_schedule_version: profile.fee_schedule_version,
            metering_schedule_version: profile.metering_schedule_version,
            budget: ResourceBudget::new_complete(
                limits.cpu_fuel,
                limits.memory_bytes,
                limits.storage_read_bytes,
                limits.storage_write_bytes,
                u32::try_from(limits.output_values).map_err(|_| MarketEvidenceError::Bounds)?,
                limits.output_bytes,
                u32::try_from(limits.table_elements).map_err(|_| MarketEvidenceError::Bounds)?,
            ),
            fees,
            fee_budget: profile.fee_budget,
            baseline_storage,
            baseline_state_root: profile.baseline_state_root,
        };
        let result = Self {
            profile,
            billing,
            catalogue: Rc::new(catalogue),
            authority,
        };
        if evidence.initial_boundary.index != 0
            || evidence.final_boundary.index.checked_add(1) != Some(result.billing.boundary_count)
        {
            return Err(MarketEvidenceError::Path);
        }
        let initial = result.boundary(evidence.initial_boundary)?;
        let final_state = result.boundary(evidence.final_boundary)?;
        if ArbitrationStepCommitment::from_state(&initial.arbitration)
            .map_err(|_| MarketEvidenceError::Identity)?
            .digest
            != result.profile.initial_execution_state_root
            || ArbitrationStepCommitment::from_state(&final_state.arbitration)
                .map_err(|_| MarketEvidenceError::Identity)?
                .digest
                != result.billing.final_execution_state_root
        {
            return Err(MarketEvidenceError::Identity);
        }
        Ok(result)
    }
    pub fn profile(&self) -> &MarketSandboxProfile {
        &self.profile
    }
    pub fn billing(&self) -> &MarketBillingCommitment {
        &self.billing
    }
    pub fn verify_step(&self, pre: &BoundaryProof, post: &BoundaryProof) -> MarketStepVerdict {
        match self.step(pre, post) {
            Ok(true) => MarketStepVerdict::Correct,
            Ok(false) => MarketStepVerdict::Incorrect,
            Err(error) => MarketStepVerdict::InvalidEvidence(error),
        }
    }
    fn step(&self, pre: &BoundaryProof, post: &BoundaryProof) -> Result<bool, MarketEvidenceError> {
        let final_trap =
            pre == post && pre.index.checked_add(1) == Some(self.billing.boundary_count);
        if !final_trap && pre.index.checked_add(1) != Some(post.index) {
            return Err(MarketEvidenceError::Path);
        }
        let pre = self.boundary(pre)?;
        let mut post = self.boundary(post)?;
        if final_trap && pre.trap.is_none() {
            return Err(MarketEvidenceError::Path);
        }
        if !final_trap && pre.trap.is_some() {
            return Err(MarketEvidenceError::Path);
        }
        let program =
            ProgramId::new(self.profile.sandbox_program).map_err(|_| MarketEvidenceError::Code)?;
        let module = self
            .catalogue
            .program_module(program)
            .ok_or(MarketEvidenceError::Code)?;
        let observed = observe_market_sandbox_step(
            module,
            &pre,
            &self.authority,
            self.profile.maximum_bytes as usize,
        )
        .map_err(|_| MarketEvidenceError::Runtime)?;
        if !final_trap && observed.trap.is_none() {
            post.trap = None;
        }
        Ok(observed
            .reencode_untrusted(self.profile.maximum_bytes as usize)
            .map_err(|_| MarketEvidenceError::Encoding)?
            == post
                .reencode_untrusted(self.profile.maximum_bytes as usize)
                .map_err(|_| MarketEvidenceError::Encoding)?)
    }
    fn boundary(&self, proof: &BoundaryProof) -> Result<PortableBoundary, MarketEvidenceError> {
        if proof.index >= self.billing.boundary_count
            || proof.leaf.len() > self.profile.maximum_bytes as usize
            || proof.siblings.len() > 12
        {
            return Err(MarketEvidenceError::Bounds);
        }
        let mut node = replay_leaf_hash(proof.index, &proof.leaf)
            .map_err(|_| MarketEvidenceError::Encoding)?;
        let mut count = self.billing.boundary_count;
        let mut index = proof.index;
        for sibling in &proof.siblings {
            if count <= 1 || ((index ^ 1) >= count && sibling != &node) {
                return Err(MarketEvidenceError::Path);
            }
            node = if index & 1 == 0 {
                replay_node_hash(node, *sibling)
            } else {
                replay_node_hash(*sibling, node)
            };
            index /= 2;
            count = count.div_ceil(2);
        }
        if count != 1 || node != self.billing.provider_trace_root {
            return Err(MarketEvidenceError::Path);
        }
        let boundary =
            PortableBoundary::decode_untrusted(&proof.leaf, self.profile.maximum_bytes as usize)
                .map_err(|_| MarketEvidenceError::Encoding)?;
        let identity = boundary.arbitration.identity;
        if identity.module_code_hash != self.profile.code_hash
            || identity.input_digest != self.profile.input_digest
            || identity.runtime_version != self.profile.runtime_version
            || identity.abi_version != self.profile.abi_version
            || identity.fee_schedule_version != self.profile.fee_schedule_version
            || identity.metering_schedule_version != self.profile.metering_schedule_version
            || identity.host_base_state_root != self.profile.baseline_state_root
            || identity.trace_policy
                != ProgramReplayProfile::new(
                    self.profile.maximum_boundaries,
                    self.profile.maximum_bytes,
                )
                .map_err(|_| MarketEvidenceError::Profile)?
                .trace_policy()
        {
            return Err(MarketEvidenceError::Identity);
        }
        Ok(boundary)
    }
}

fn check_receipt(
    receipt: &VerifiedReceipt,
    admission: &VerifiedAdmissionPrestate,
) -> Result<(), MarketEvidenceError> {
    let value = receipt
        .receipt()
        .protocol()
        .ok_or(MarketEvidenceError::Receipt)?;
    if value.protocol_version() != 3
        || value.module_id() != 9
        || value.operation() != 3
        || value.result_code() != 0
        || value.activity_id() != admission.activity_id()
        || value.global_sequence() != admission.execution_sequence()
        || value.previous_state_root() != admission.state_root()
        || receipt.evidence().receipt_digest() != Some(admission.receipt_digest())
    {
        return Err(MarketEvidenceError::Receipt);
    }
    Ok(())
}
fn market_calldata(
    admission: &VerifiedAdmissionPrestate,
    expected: [u8; 32],
) -> Result<&[u8], MarketEvidenceError> {
    let mut call = admission.activity().payload();
    if call.len() >= 34 && call[..34] == [0; 34] {
        let mut profile = Reader(call);
        profile.take(34)?;
        if profile.take(30)? != b"LXP/program-replay-profile/v1\0" || profile.u16()? != 1 {
            return Err(MarketEvidenceError::Encoding);
        }
        profile.u32()?;
        profile.u32()?;
        call = profile.vector(MAX_BYTES)?;
        if !profile.done() {
            return Err(MarketEvidenceError::Encoding);
        }
    }
    if call.len() < 106 || call[..32] != expected {
        return Err(MarketEvidenceError::Admission);
    }
    let entry = u16::from_be_bytes(
        call[34..36]
            .try_into()
            .map_err(|_| MarketEvidenceError::Encoding)?,
    ) as usize;
    let data = u32::from_be_bytes(
        call[36..40]
            .try_into()
            .map_err(|_| MarketEvidenceError::Encoding)?,
    ) as usize;
    let caps = u16::from_be_bytes(
        call[40..42]
            .try_into()
            .map_err(|_| MarketEvidenceError::Encoding)?,
    ) as usize;
    let access = u32::from_be_bytes(
        call[42..46]
            .try_into()
            .map_err(|_| MarketEvidenceError::Encoding)?,
    ) as usize;
    let start = 106usize
        .checked_add(entry)
        .ok_or(MarketEvidenceError::Bounds)?;
    let end = start.checked_add(data).ok_or(MarketEvidenceError::Bounds)?;
    if end.checked_add(caps).and_then(|n| n.checked_add(access)) != Some(call.len()) {
        return Err(MarketEvidenceError::Encoding);
    }
    call.get(start..end).ok_or(MarketEvidenceError::Encoding)
}
fn cell<'a>(
    records: &'a BTreeMap<Vec<u8>, Vec<u8>>,
    prefix: &[u8],
    id: [u8; 32],
) -> Result<&'a [u8], MarketEvidenceError> {
    let mut key = prefix.to_vec();
    key.extend_from_slice(&id);
    records
        .get(&key)
        .map(Vec::as_slice)
        .ok_or(MarketEvidenceError::Storage)
}
fn selected_fees(
    admission: &VerifiedAdmissionPrestate,
    version: u32,
    batch: u64,
) -> Result<FeeSchedule, MarketEvidenceError> {
    let mut key = b"progfee/history/v1/".to_vec();
    key.extend_from_slice(&version.to_be_bytes());
    let value = admission
        .legacy()
        .legacy()
        .program_records()
        .get(&key)
        .ok_or(MarketEvidenceError::Admission)?;
    if value.len() != 217 || &value[..5] != b"LXFR1" {
        return Err(MarketEvidenceError::Admission);
    }
    let activation = u64::from_be_bytes(
        value[145..153]
            .try_into()
            .map_err(|_| MarketEvidenceError::Admission)?,
    );
    if activation == 0 || activation > batch {
        return Err(MarketEvidenceError::Admission);
    }
    let mut reader = Reader(&value[5..]);
    if reader.u32()? != version {
        return Err(MarketEvidenceError::Admission);
    }
    let fees = FeeSchedule::new_complete(FeeScheduleParameters {
        version,
        fee_units_per_cpu_fuel: reader.u64()?,
        fee_units_per_memory_byte: reader.u64()?,
        fee_units_per_storage_read_byte: reader.u64()?,
        fee_units_per_storage_write_byte: reader.u64()?,
        fee_units_per_output_value: reader.u64()?,
        fee_units_per_output_byte: reader.u64()?,
        fee_units_per_occupancy_byte_batch: reader.u64()?,
    });
    if !fees.is_valid() {
        return Err(MarketEvidenceError::Admission);
    }
    Ok(fees)
}
fn digest(bytes: &[u8]) -> [u8; 32] {
    Sha256::digest(bytes).into()
}
struct Reader<'a>(&'a [u8]);
impl<'a> Reader<'a> {
    fn take(&mut self, n: usize) -> Result<&'a [u8], MarketEvidenceError> {
        let out = self.0.get(..n).ok_or(MarketEvidenceError::Encoding)?;
        self.0 = &self.0[n..];
        Ok(out)
    }
    fn array<const N: usize>(&mut self) -> Result<[u8; N], MarketEvidenceError> {
        self.take(N)?
            .try_into()
            .map_err(|_| MarketEvidenceError::Encoding)
    }
    fn u16(&mut self) -> Result<u16, MarketEvidenceError> {
        Ok(u16::from_be_bytes(self.array()?))
    }
    fn u32(&mut self) -> Result<u32, MarketEvidenceError> {
        Ok(u32::from_be_bytes(self.array()?))
    }
    fn u64(&mut self) -> Result<u64, MarketEvidenceError> {
        Ok(u64::from_be_bytes(self.array()?))
    }
    fn vector(&mut self, max: usize) -> Result<&'a [u8], MarketEvidenceError> {
        let size = self.u32()? as usize;
        if size > max {
            return Err(MarketEvidenceError::Bounds);
        }
        self.take(size)
    }
    fn done(&self) -> bool {
        self.0.is_empty()
    }
}
