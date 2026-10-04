use std::collections::BTreeMap;
use std::rc::Rc;

use layerx_client::evidence::VerifiedAdmissionPrestate;
use layerx_programs::{interface_state_key, ProgramInterface};
use layerx_programs_runtime::execute::{replay_portable_step, PortableReplayInputs};
use layerx_programs_runtime::portable_replay::PortableBoundary;
use layerx_programs_runtime::{
    AbiError, AbiRevision, ActivityBudgetBinding, CapabilitySet, CompositionRefusal, ProgramId,
    ProgramReplayProfile, ProgramResolver, ValidatedModule, WasmEngine,
};
use layerx_proof::receipt::VerifiedReceipt;
use layerx_proof::state_range::ModuleRangeWitness;
use layerx_proof::state_witness::StateWitness;
use sha2::{Digest, Sha256};

const MAX_BYTES: usize = 1_048_576;
const RECORD_DOMAIN: &[u8] = b"LXP/program-replay-native/v1\0";
const AUTHORITY_DOMAIN: &[u8] = b"LXP/program-replay-authority/v1\0";
const PROFILE_DOMAIN: &[u8] = b"LXP/program-replay-profile/v1\0";

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum ReplayError {
    Bounds,
    Encoding,
    Receipt,
    Admission,
    Metadata,
    Authority,
    Catalogue,
    Code,
    Lifecycle,
    Interface,
    Leaf,
    Path,
    Adjacency,
    Identity,
    Runtime,
}
impl std::fmt::Display for ReplayError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{self:?}")
    }
}
impl std::error::Error for ReplayError {}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct ReplayMetadata {
    pub network_id: u32,
    pub activity_id: [u8; 32],
    pub sequence: u64,
    pub previous_root: [u8; 32],
    pub program_id: [u8; 32],
    pub code_hash: [u8; 32],
    pub input_digest: [u8; 32],
    pub runtime_version: u16,
    pub abi_version: u16,
    pub fee_version: u32,
    pub metering_version: u32,
    pub maximum_boundaries: u32,
    pub maximum_bytes: u32,
    pub terminal_status: u8,
    pub boundary_count: u32,
    pub authority_root: [u8; 32],
    pub hosts_root: [u8; 32],
    pub boundary_root: [u8; 32],
    pub witness_digest: [u8; 32],
    pub result_code: i32,
    pub blob_digest: [u8; 32],
}
impl ReplayMetadata {
    pub fn decode_untrusted(bytes: &[u8]) -> Result<Self, ReplayError> {
        if bytes.len() != 394 {
            return Err(ReplayError::Metadata);
        }
        let mut r = Reader(bytes);
        r.domain(RECORD_DOMAIN)?;
        if r.u16()? != 1 {
            return Err(ReplayError::Encoding);
        }
        let network_id = r.u32()?;
        let activity_id = r.array()?;
        let sequence = r.u64()?;
        let previous_root = r.array()?;
        let program_id = r.array()?;
        let code_hash = r.array()?;
        let input_digest = r.array()?;
        let runtime_version = r.u16()?;
        let abi_version = r.u16()?;
        let fee_version = r.u32()?;
        let metering_version = r.u32()?;
        if r.u16()? != 1 {
            return Err(ReplayError::Encoding);
        }
        let maximum_boundaries = r.u32()?;
        let maximum_bytes = r.u32()?;
        let terminal_status = r.array::<1>()?[0];
        let boundary_count = r.u32()?;
        let value = Self {
            network_id,
            activity_id,
            sequence,
            previous_root,
            program_id,
            code_hash,
            input_digest,
            runtime_version,
            abi_version,
            fee_version,
            metering_version,
            maximum_boundaries,
            maximum_bytes,
            terminal_status,
            boundary_count,
            authority_root: r.array()?,
            hosts_root: r.array()?,
            boundary_root: r.array()?,
            witness_digest: r.array()?,
            result_code: r.u32()? as i32,
            blob_digest: r.array()?,
        };
        if !r.done()
            || value.network_id == 0
            || value.sequence == 0
            || value.activity_id == [0; 32]
            || value.program_id == [0; 32]
            || value.code_hash == [0; 32]
            || value.previous_root == [0; 32]
            || value.terminal_status > 2
            || value.maximum_boundaries > 4096
            || value.boundary_count == 0
            || value.boundary_count > value.maximum_boundaries
            || ProgramReplayProfile::new(value.maximum_boundaries, value.maximum_bytes).is_err()
        {
            return Err(ReplayError::Metadata);
        }
        Ok(value)
    }
}

#[derive(Debug)]
struct CatalogueEntry {
    module: ValidatedModule,
    interface: Option<ProgramInterface>,
}

#[derive(Debug)]
pub struct AuthenticatedCatalogue {
    network_id: u32,
    activity_id: [u8; 32],
    sequence: u64,
    previous_root: [u8; 32],
    receipt_digest: [u8; 32],
    activity_binding: Option<ActivityBudgetBinding>,
    modules: BTreeMap<ProgramId, CatalogueEntry>,
}
impl AuthenticatedCatalogue {
    pub fn verify(
        admission: &VerifiedAdmissionPrestate,
        code_blobs: &BTreeMap<[u8; 32], Vec<u8>>,
    ) -> Result<Self, ReplayError> {
        let records: BTreeMap<&[u8], &[u8]> = admission
            .legacy()
            .legacy()
            .program_records()
            .iter()
            .map(|(key, value)| (key.as_slice(), value.as_slice()))
            .collect();
        let engine = WasmEngine::declared().map_err(|_| ReplayError::Code)?;
        let mut modules = BTreeMap::new();
        for (key, record) in &records {
            if !key.starts_with(b"program\0") {
                continue;
            }
            if key.len() != 40 || record.len() != 71 {
                return Err(ReplayError::Catalogue);
            }
            let program = ProgramId::new(key[8..].try_into().map_err(|_| ReplayError::Catalogue)?)
                .map_err(|_| ReplayError::Catalogue)?;
            let authority = &record[1..33];
            if !((record[0] == 0 && authority == [0; 32])
                || (record[0] == 1 && authority != [0; 32]))
            {
                return Err(ReplayError::Catalogue);
            }
            let hash: [u8; 32] = record[33..65]
                .try_into()
                .map_err(|_| ReplayError::Catalogue)?;
            let abi = u16::from_be_bytes(
                record[65..67]
                    .try_into()
                    .map_err(|_| ReplayError::Catalogue)?,
            );
            let version = u32::from_be_bytes(
                record[67..71]
                    .try_into()
                    .map_err(|_| ReplayError::Catalogue)?,
            );
            if hash == [0; 32] || version == 0 {
                return Err(ReplayError::Catalogue);
            }
            let mut status_key = b"wind-down\0s".to_vec();
            status_key.extend_from_slice(&program.bytes());
            if let Some(status) = records.get(status_key.as_slice()) {
                if status.len() != 54
                    || status[0] != 1
                    || status[2..34] != program.bytes()
                    || !matches!(status[1], 2 | 3)
                    || status[34..42] == [0; 8]
                    || status[42..50] == [0; 8]
                {
                    return Err(ReplayError::Lifecycle);
                }
                continue;
            }
            let bytes = code_blobs.get(&hash).ok_or(ReplayError::Code)?;
            if bytes.is_empty() || bytes.len() > MAX_BYTES || digest(bytes) != hash {
                return Err(ReplayError::Code);
            }
            let module = engine
                .validate_versioned(abi, bytes)
                .map_err(|_| ReplayError::Code)?;
            if module.code_hash() != hash {
                return Err(ReplayError::Code);
            }
            let interface =
                if let Some(value) = records.get(interface_state_key(program).as_slice()) {
                    let mut reader = Reader(value);
                    if reader.array::<32>()? != program.bytes() || reader.u32()? != version {
                        return Err(ReplayError::Interface);
                    }
                    let declared_digest: [u8; 32] = reader.array()?;
                    let encoded = reader.vector(MAX_BYTES)?;
                    if !reader.done() || digest(encoded) != declared_digest {
                        return Err(ReplayError::Interface);
                    }
                    let interface =
                        ProgramInterface::decode(encoded).map_err(|_| ReplayError::Interface)?;
                    interface
                        .require_module(bytes, abi)
                        .map_err(|_| ReplayError::Interface)?;
                    Some(interface)
                } else {
                    None
                };
            modules.insert(program, CatalogueEntry { module, interface });
        }
        Ok(Self {
            network_id: admission.network_id(),
            activity_id: admission.activity_id(),
            sequence: admission.execution_sequence(),
            previous_root: admission.state_root(),
            receipt_digest: admission.receipt_digest(),
            activity_binding: None,
            modules,
        })
    }
    pub fn verify_with_module(
        admission: &VerifiedAdmissionPrestate,
        full_module9: &ModuleRangeWitness,
        code_blobs: &BTreeMap<[u8; 32], Vec<u8>>,
    ) -> Result<Self, ReplayError> {
        if full_module9.module_id != 9 || full_module9.composite_index != 9 {
            return Err(ReplayError::Catalogue);
        }
        let inventory = full_module9
            .verify_full_module(admission.state_root())
            .map_err(|_| ReplayError::Catalogue)?;
        let records: BTreeMap<Vec<u8>, Vec<u8>> = inventory.records().iter().cloned().collect();
        if &records != admission.legacy().legacy().program_records() {
            return Err(ReplayError::Catalogue);
        }
        Self::verify(admission, code_blobs)
    }
}
impl ProgramResolver for AuthenticatedCatalogue {
    fn authorize_activity(
        &self,
        binding: Option<ActivityBudgetBinding>,
    ) -> Result<(), CompositionRefusal> {
        if binding.is_none() || binding != self.activity_binding {
            return Err(CompositionRefusal::ActivityEvidenceMismatch);
        }
        Ok(())
    }
    fn program_module(&self, program: ProgramId) -> Option<&ValidatedModule> {
        self.modules.get(&program).map(|entry| &entry.module)
    }
    fn authorize_interface_call(
        &self,
        program: ProgramId,
        entrypoint: &str,
        input: &[u8],
        capabilities: &CapabilitySet,
    ) -> Result<CapabilitySet, AbiError> {
        let entry = self
            .modules
            .get(&program)
            .ok_or(AbiError::InvalidEncoding)?;
        if let Some(interface) = &entry.interface {
            let (declared, _) = interface
                .decode_call(input)
                .map_err(|_| AbiError::InvalidEncoding)?;
            if declared.name != entrypoint {
                return Err(AbiError::InvalidEncoding);
            }
            let encoding = capabilities.canonical_encoding();
            let grants = if entry.module.abi_revision() == AbiRevision::V1 {
                CapabilitySet::decode_canonical(&encoding)?
            } else {
                CapabilitySet::decode_v2_canonical(&encoding)?
            };
            interface
                .authorize_call(program, input, &grants)
                .map_err(|_| AbiError::InvalidEncoding)?;
        }
        Ok(capabilities.clone())
    }
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct BoundaryProof {
    pub index: u32,
    pub leaf: Vec<u8>,
    pub siblings: Vec<[u8; 32]>,
}
impl BoundaryProof {
    fn verify(&self, metadata: &ReplayMetadata) -> Result<PortableBoundary, ReplayError> {
        if self.index >= metadata.boundary_count
            || self.leaf.is_empty()
            || self.leaf.len() > metadata.maximum_bytes as usize
            || self.siblings.len() > 12
        {
            return Err(ReplayError::Bounds);
        }
        let mut hash = Sha256::new();
        hash.update(b"LXP/program-replay-leaf/v1\0");
        hash.update(self.index.to_be_bytes());
        hash.update((self.leaf.len() as u32).to_be_bytes());
        hash.update(&self.leaf);
        let mut node: [u8; 32] = hash.finalize().into();
        let mut index = self.index;
        let mut count = metadata.boundary_count;
        for sibling in &self.siblings {
            if count <= 1 || ((index ^ 1) >= count && sibling != &node) {
                return Err(ReplayError::Path);
            }
            let mut hash = Sha256::new();
            hash.update(b"LXP/program-replay-node/v1\0");
            if index & 1 == 0 {
                hash.update(node);
                hash.update(sibling);
            } else {
                hash.update(sibling);
                hash.update(node);
            }
            node = hash.finalize().into();
            index /= 2;
            count = count.div_ceil(2);
        }
        if count != 1 || node != metadata.boundary_root {
            return Err(ReplayError::Path);
        }
        let boundary =
            PortableBoundary::decode_untrusted(&self.leaf, metadata.maximum_bytes as usize)
                .map_err(|_| ReplayError::Leaf)?;
        let identity = boundary.arbitration.identity;
        if identity.module_code_hash != metadata.code_hash
            || identity.input_digest != metadata.input_digest
            || identity.runtime_version != metadata.runtime_version
            || identity.abi_version != metadata.abi_version
            || identity.fee_schedule_version != metadata.fee_version
            || identity.metering_schedule_version != metadata.metering_version
            || identity.trace_policy
                != ProgramReplayProfile::new(metadata.maximum_boundaries, metadata.maximum_bytes)
                    .map_err(|_| ReplayError::Metadata)?
                    .trace_policy()
        {
            return Err(ReplayError::Identity);
        }
        Ok(boundary)
    }
}

pub struct VerifiedReplay {
    metadata: ReplayMetadata,
    catalogue: Rc<AuthenticatedCatalogue>,
    inputs: PortableReplayInputs,
}
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct VerifiedStep {
    activity_id: [u8; 32],
    pre_index: u32,
    post_index: u32,
}
impl VerifiedStep {
    pub const fn activity_id(self) -> [u8; 32] {
        self.activity_id
    }
    pub const fn pre_index(self) -> u32 {
        self.pre_index
    }
    pub const fn post_index(self) -> u32 {
        self.post_index
    }
}
impl VerifiedReplay {
    pub fn verify(
        receipt: &VerifiedReceipt,
        admission: &VerifiedAdmissionPrestate,
        metadata_proof: &StateWitness,
        authority_bytes: &[u8],
        hosts_bytes: &[u8],
        mut catalogue: AuthenticatedCatalogue,
    ) -> Result<Self, ReplayError> {
        let unsigned = layerx_wire::receipt::encode_unsigned(receipt.receipt())
            .map_err(|_| ReplayError::Receipt)?;
        let expected_digest =
            layerx_wire::hash::receipt_digest(&unsigned).map_err(|_| ReplayError::Receipt)?;
        if expected_digest != admission.receipt_digest() {
            return Err(ReplayError::Admission);
        }
        let receipt = receipt.receipt().protocol().ok_or(ReplayError::Receipt)?;
        let metadata = ReplayMetadata::decode_untrusted(&metadata_proof.value)?;
        let mut expected_key = b"progreplay/v1/".to_vec();
        expected_key.extend_from_slice(&receipt.activity_id());
        if metadata_proof.module_id != 9
            || metadata_proof.key != expected_key
            || metadata_proof
                .verify(receipt.resulting_state_root())
                .is_err()
        {
            return Err(ReplayError::Metadata);
        }
        if receipt.protocol_version() != 3
            || receipt.module_id() != 9
            || receipt.operation() != 3
            || receipt.activity_id() != metadata.activity_id
            || receipt.global_sequence() != metadata.sequence
            || receipt.previous_state_root() != metadata.previous_root
            || receipt.result_code() != metadata.result_code
            || admission.activity_id() != metadata.activity_id
            || admission.execution_sequence() != metadata.sequence
            || admission.network_id() != metadata.network_id
            || admission.state_root() != metadata.previous_root
            || catalogue.network_id != metadata.network_id
            || catalogue.activity_id != metadata.activity_id
            || catalogue.sequence != metadata.sequence
            || catalogue.previous_root != metadata.previous_root
            || catalogue.receipt_digest != admission.receipt_digest()
        {
            return Err(ReplayError::Admission);
        }
        let outcome = receipt.program_outcome().ok_or(ReplayError::Receipt)?;
        if outcome.runtime_version() != metadata.runtime_version
            || outcome.abi_version() != metadata.abi_version
            || outcome.fee_schedule_version() != metadata.fee_version
            || outcome.metering_schedule_version() != metadata.metering_version
            || outcome.terminal_kind() != metadata.terminal_status + 1
        {
            return Err(ReplayError::Receipt);
        }
        if authority_bytes.len() > metadata.maximum_bytes as usize
            || hosts_bytes.len() > metadata.maximum_bytes as usize
            || digest(authority_bytes) != metadata.authority_root
            || digest(hosts_bytes) != metadata.hosts_root
        {
            return Err(ReplayError::Authority);
        }
        let mut authority = Reader(authority_bytes);
        authority.domain(AUTHORITY_DOMAIN)?;
        let signed = authority.vector(MAX_BYTES)?;
        if layerx_wire::activity::encode_signed(admission.activity())
            .map_err(|_| ReplayError::Authority)?
            != signed
        {
            return Err(ReplayError::Authority);
        }
        let mut profile = Reader(admission.activity().payload());
        if profile.array::<34>()? != [0; 34] {
            return Err(ReplayError::Admission);
        }
        profile.domain(PROFILE_DOMAIN)?;
        if profile.u16()? != 1
            || profile.u32()? != metadata.maximum_boundaries
            || profile.u32()? != metadata.maximum_bytes
        {
            return Err(ReplayError::Admission);
        }
        let call = profile.vector(MAX_BYTES)?;
        if !profile.done()
            || call.len() < 106
            || call[..32] != metadata.program_id
            || u16::from_be_bytes(
                call[32..34]
                    .try_into()
                    .map_err(|_| ReplayError::Admission)?,
            ) != metadata.abi_version
        {
            return Err(ReplayError::Admission);
        }
        let program = ProgramId::new(metadata.program_id).map_err(|_| ReplayError::Code)?;
        let module = catalogue
            .program_module(program)
            .ok_or(ReplayError::Lifecycle)?;
        if module.code_hash() != metadata.code_hash
            || module.metering_schedule_version() != metadata.metering_version
        {
            return Err(ReplayError::Code);
        }
        let footer = authority_bytes
            .len()
            .checked_sub(272)
            .ok_or(ReplayError::Authority)?;
        catalogue.activity_binding = Some(
            ActivityBudgetBinding::new(
                authority_bytes
                    .get(footer..footer + 32)
                    .ok_or(ReplayError::Authority)?
                    .try_into()
                    .map_err(|_| ReplayError::Authority)?,
            )
            .map_err(|_| ReplayError::Authority)?,
        );
        let catalogue = Rc::new(catalogue);
        let resolver: Rc<dyn ProgramResolver> = catalogue.clone();
        let inputs = PortableReplayInputs::new(
            authority_bytes,
            hosts_bytes,
            Some(resolver),
            metadata.maximum_bytes as usize,
        )
        .map_err(|_| ReplayError::Authority)?;
        Ok(Self {
            metadata,
            catalogue,
            inputs,
        })
    }
    pub fn metadata(&self) -> &ReplayMetadata {
        &self.metadata
    }
    pub fn verify_step(
        &self,
        pre: &BoundaryProof,
        post: &BoundaryProof,
    ) -> Result<VerifiedStep, ReplayError> {
        let before = pre.verify(&self.metadata)?;
        let after = post.verify(&self.metadata)?;
        let trap = pre.index == post.index
            && pre == post
            && before.trap.is_some()
            && pre.index + 1 == self.metadata.boundary_count
            && self.metadata.terminal_status != 0;
        if !trap && (pre.index.checked_add(1) != Some(post.index) || before.trap.is_some()) {
            return Err(ReplayError::Adjacency);
        }
        let program = ProgramId::new(self.metadata.program_id).map_err(|_| ReplayError::Code)?;
        let module = self
            .catalogue
            .program_module(program)
            .ok_or(ReplayError::Code)?;
        replay_portable_step(module, &before, &after, &self.inputs)
            .map_err(|_| ReplayError::Runtime)?;
        Ok(VerifiedStep {
            activity_id: self.metadata.activity_id,
            pre_index: pre.index,
            post_index: post.index,
        })
    }
}

fn digest(bytes: &[u8]) -> [u8; 32] {
    Sha256::digest(bytes).into()
}
struct Reader<'a>(&'a [u8]);
impl<'a> Reader<'a> {
    fn take(&mut self, n: usize) -> Result<&'a [u8], ReplayError> {
        let out = self.0.get(..n).ok_or(ReplayError::Encoding)?;
        self.0 = &self.0[n..];
        Ok(out)
    }
    fn array<const N: usize>(&mut self) -> Result<[u8; N], ReplayError> {
        self.take(N)?.try_into().map_err(|_| ReplayError::Encoding)
    }
    fn u16(&mut self) -> Result<u16, ReplayError> {
        Ok(u16::from_be_bytes(self.array()?))
    }
    fn u32(&mut self) -> Result<u32, ReplayError> {
        Ok(u32::from_be_bytes(self.array()?))
    }
    fn u64(&mut self) -> Result<u64, ReplayError> {
        Ok(u64::from_be_bytes(self.array()?))
    }
    fn vector(&mut self, max: usize) -> Result<&'a [u8], ReplayError> {
        let len = self.u32()? as usize;
        if len > max {
            return Err(ReplayError::Bounds);
        }
        self.take(len)
    }
    fn domain(&mut self, domain: &[u8]) -> Result<(), ReplayError> {
        if self.take(domain.len())? != domain {
            return Err(ReplayError::Encoding);
        }
        Ok(())
    }
    fn done(&self) -> bool {
        self.0.is_empty()
    }
}
