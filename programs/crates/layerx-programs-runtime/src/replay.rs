use sha2::{Digest, Sha256};

const DOMAIN: &[u8] = b"LayerX/programs/replay-host-witness/v1\0";
pub(crate) const STORAGE_DOMAIN: &[u8] = b"LayerX/programs/v2/storage-root\0";
const ABI_DOMAIN: &[u8] = b"LayerX/programs/v2/host-state\0";
const RUNTIME_DOMAIN: &[u8] = b"LayerX/programs/v2/runtime-host-state\0";
const ISOLATED_DOMAIN: &[u8] = b"LayerX/programs/v2/isolated-host-state\0";

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ReplayWitnessError { Bounds, Allocation, Encoding, Binding, StateUnavailable }

pub(crate) struct ReplayCursor<'a> { bytes: &'a [u8], offset: usize }
impl<'a> ReplayCursor<'a> {
    pub(crate) fn new(bytes: &'a [u8]) -> Self { Self { bytes, offset: 0 } }
    pub(crate) fn take(&mut self, length: usize) -> Result<&'a [u8], ReplayWitnessError> {
        let end = self.offset.checked_add(length).ok_or(ReplayWitnessError::Bounds)?;
        let value = self.bytes.get(self.offset..end).ok_or(ReplayWitnessError::Encoding)?;
        self.offset = end;
        Ok(value)
    }
    pub(crate) fn u8(&mut self) -> Result<u8, ReplayWitnessError> { Ok(self.take(1)?[0]) }
    pub(crate) fn u32(&mut self) -> Result<u32, ReplayWitnessError> {
        Ok(u32::from_be_bytes(self.take(4)?.try_into().map_err(|_| ReplayWitnessError::Encoding)?))
    }
    pub(crate) fn u64(&mut self) -> Result<u64, ReplayWitnessError> {
        Ok(u64::from_be_bytes(self.take(8)?.try_into().map_err(|_| ReplayWitnessError::Encoding)?))
    }
    pub(crate) fn u128(&mut self) -> Result<u128, ReplayWitnessError> {
        Ok(u128::from_be_bytes(self.take(16)?.try_into().map_err(|_| ReplayWitnessError::Encoding)?))
    }
    pub(crate) fn field(&mut self) -> Result<&'a [u8], ReplayWitnessError> {
        let length = usize::try_from(self.u32()?).map_err(|_| ReplayWitnessError::Bounds)?;
        self.take(length)
    }
    pub(crate) fn done(&self) -> bool { self.offset == self.bytes.len() }
}

pub(crate) fn append(out: &mut Vec<u8>, bytes: &[u8], maximum: usize) -> Result<(), ReplayWitnessError> {
    let length = out.len().checked_add(bytes.len()).ok_or(ReplayWitnessError::Bounds)?;
    if length > maximum { return Err(ReplayWitnessError::Bounds); }
    out.try_reserve_exact(bytes.len()).map_err(|_| ReplayWitnessError::Allocation)?;
    out.extend_from_slice(bytes);
    Ok(())
}
fn copy(bytes: &[u8]) -> Result<Vec<u8>, ReplayWitnessError> {
    let mut out = Vec::new(); append(&mut out, bytes, bytes.len())?; Ok(out)
}
fn field(out: &mut Vec<u8>, bytes: &[u8], maximum: usize) -> Result<(), ReplayWitnessError> {
    append(out, &u32::try_from(bytes.len()).map_err(|_| ReplayWitnessError::Bounds)?.to_be_bytes(), maximum)?;
    append(out, bytes, maximum)
}
pub(crate) fn maximum_bytes(maximum: usize) -> Result<(), ReplayWitnessError> {
    if maximum == 0 || maximum as u64 > crate::MAX_TRACE_STATE_BYTES { return Err(ReplayWitnessError::Bounds); }
    Ok(())
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ReplayHostWitnessV1 {
    code_hash: [u8; 32],
    abi: Option<Vec<u8>>,
    runtime: Vec<u8>,
    meter: Vec<u8>,
    storage_baseline: Vec<u8>,
}

impl ReplayHostWitnessV1 {
    pub(crate) fn payload_budget(maximum: usize, has_abi: bool) -> Result<usize, ReplayWitnessError> {
        maximum_bytes(maximum)?;
        maximum.checked_sub(DOMAIN.len() + 32 + 1 + 12 + if has_abi { 4 } else { 0 }).ok_or(ReplayWitnessError::Bounds)
    }
    pub(crate) fn from_parts(code_hash: [u8; 32], abi: Option<Vec<u8>>, runtime: Vec<u8>, meter: Vec<u8>, storage_baseline: Vec<u8>, maximum: usize) -> Result<Self, ReplayWitnessError> {
        maximum_bytes(maximum)?;
        let value = Self { code_hash, abi, runtime, meter, storage_baseline };
        if value.encoded_len()? > maximum { return Err(ReplayWitnessError::Bounds); }
        value.validate_structure()?;
        Ok(value)
    }
    fn encoded_len(&self) -> Result<usize, ReplayWitnessError> {
        let mut length = DOMAIN.len().checked_add(32 + 1 + 12).ok_or(ReplayWitnessError::Bounds)?;
        for bytes in [&self.runtime, &self.meter, &self.storage_baseline] {
            length = length.checked_add(bytes.len()).ok_or(ReplayWitnessError::Bounds)?;
        }
        if let Some(abi) = &self.abi { length = length.checked_add(4).and_then(|n| n.checked_add(abi.len())).ok_or(ReplayWitnessError::Bounds)?; }
        Ok(length)
    }
    fn validate_structure(&self) -> Result<(), ReplayWitnessError> {
        if self.code_hash == [0; 32] { return Err(ReplayWitnessError::Binding); }
        let _ = crate::Meter::from_replay_state_bytes(&self.meter)?;
        let _ = self.v2_preimage_commitment()?;
        validate_storage_preimage(&self.storage_baseline)?;
        Ok(())
    }
    pub fn canonical_bytes(&self, maximum: usize) -> Result<Vec<u8>, ReplayWitnessError> {
        maximum_bytes(maximum)?;
        let length = self.encoded_len()?;
        if length > maximum { return Err(ReplayWitnessError::Bounds); }
        let mut out = Vec::new();
        out.try_reserve_exact(length).map_err(|_| ReplayWitnessError::Allocation)?;
        append(&mut out, DOMAIN, maximum)?;
        append(&mut out, &self.code_hash, maximum)?;
        match &self.abi {
            None => append(&mut out, &[0], maximum)?,
            Some(abi) => { append(&mut out, &[1], maximum)?; field(&mut out, abi, maximum)?; }
        }
        field(&mut out, &self.runtime, maximum)?;
        field(&mut out, &self.meter, maximum)?;
        field(&mut out, &self.storage_baseline, maximum)?;
        Ok(out)
    }
    pub fn decode(encoded: &[u8], maximum: usize) -> Result<Self, ReplayWitnessError> {
        maximum_bytes(maximum)?;
        if encoded.len() > maximum { return Err(ReplayWitnessError::Bounds); }
        let mut cursor = ReplayCursor::new(encoded);
        if cursor.take(DOMAIN.len())? != DOMAIN { return Err(ReplayWitnessError::Encoding); }
        let code_hash = cursor.take(32)?.try_into().map_err(|_| ReplayWitnessError::Encoding)?;
        let abi = match cursor.u8()? { 0 => None, 1 => Some(cursor.field()?), _ => return Err(ReplayWitnessError::Encoding) };
        let runtime = cursor.field()?;
        let meter = cursor.field()?;
        let storage_baseline = cursor.field()?;
        if !cursor.done() { return Err(ReplayWitnessError::Encoding); }
        Self::from_parts(code_hash, abi.map(copy).transpose()?, copy(runtime)?, copy(meter)?, copy(storage_baseline)?, maximum)
    }
    pub fn proposal_digest(&self, maximum: usize) -> Result<[u8; 32], ReplayWitnessError> {
        Ok(Sha256::digest(self.canonical_bytes(maximum)?).into())
    }
    pub fn code_hash(&self) -> [u8; 32] { self.code_hash }
    pub fn decode_untrusted_meter(&self) -> Result<crate::Meter, ReplayWitnessError> {
        crate::Meter::from_replay_state_bytes(&self.meter)
    }
    fn v2_preimage_commitment(&self) -> Result<([u8; 32], u64), ReplayWitnessError> {
        match &self.abi {
            None => {
                if self.runtime != ISOLATED_DOMAIN || self.storage_baseline != STORAGE_DOMAIN { return Err(ReplayWitnessError::Encoding); }
                Ok((Sha256::digest(&self.runtime).into(), self.runtime.len() as u64))
            }
            Some(abi) => {
                if !abi.starts_with(ABI_DOMAIN) || abi.len() > crate::MAX_ARBITRATION_HOST_STATE_BYTES { return Err(ReplayWitnessError::Bounds); }
                let mut runtime = ReplayCursor::new(&self.runtime);
                if runtime.take(RUNTIME_DOMAIN.len())? != RUNTIME_DOMAIN { return Err(ReplayWitnessError::Encoding); }
                let abi_root: [u8; 32] = Sha256::digest(abi).into();
                if runtime.take(32)? != abi_root || runtime.u64()? != abi.len() as u64 { return Err(ReplayWitnessError::Binding); }
                let usage = crate::MeteredUsage {
                    cpu_fuel: runtime.u64()?, memory_bytes: runtime.u64()?, storage_read_bytes: runtime.u64()?, storage_write_bytes: runtime.u64()?,
                    output_values: runtime.u32()?, output_bytes: runtime.u64()?, occupancy_byte_batches: runtime.u128()?,
                    occupancy_fee_units: runtime.u128()?, fee_units: runtime.u128()?,
                };
                let remaining_cpu = runtime.u64()?;
                let meter = crate::Meter::from_replay_state_bytes(&self.meter)?;
                if meter.execution_trace_usage().map_err(|_| ReplayWitnessError::StateUnavailable)? != usage || meter.cpu_remaining() != remaining_cpu {
                    return Err(ReplayWitnessError::Binding);
                }
                let bytes = abi.len().checked_add(self.runtime.len()).ok_or(ReplayWitnessError::Bounds)?;
                if bytes > crate::MAX_ARBITRATION_HOST_STATE_BYTES { return Err(ReplayWitnessError::Bounds); }
                Ok((Sha256::digest(&self.runtime).into(), bytes as u64))
            }
        }
    }
    pub fn compare_v2_preimages(&self, code_hash: [u8; 32], host_root: [u8; 32], host_bytes: u64, base_root: [u8; 32]) -> Result<(), ReplayWitnessError> {
        let (actual_root, actual_bytes) = self.v2_preimage_commitment()?;
        let actual_base: [u8; 32] = if self.abi.is_some() { Sha256::digest(&self.storage_baseline).into() }
            else { Sha256::digest(b"LayerX/programs/v2/isolated-base-state\0").into() };
        if self.code_hash != code_hash || actual_root != host_root || actual_bytes != host_bytes || actual_base != base_root { return Err(ReplayWitnessError::Binding); }
        Ok(())
    }
}

fn validate_storage_preimage(bytes: &[u8]) -> Result<(), ReplayWitnessError> {
    let mut cursor = ReplayCursor::new(bytes);
    if cursor.take(STORAGE_DOMAIN.len())? != STORAGE_DOMAIN { return Err(ReplayWitnessError::Encoding); }
    let mut previous: Option<(&[u8], &[u8])> = None;
    while !cursor.done() {
        let address = cursor.field()?;
        let value = cursor.field()?;
        let mut address_cursor = ReplayCursor::new(address);
        let namespace_length = u16::from_be_bytes(address_cursor.take(2)?.try_into().map_err(|_| ReplayWitnessError::Encoding)?) as usize;
        let namespace = address_cursor.take(namespace_length)?;
        if namespace.len() != 33 && namespace.len() != 65 { return Err(ReplayWitnessError::Encoding); }
        if namespace[..32] == [0; 32] { return Err(ReplayWitnessError::Encoding); }
        match namespace[32] {
            0 if namespace.len() == 65 && namespace[33..] != [0; 32] => {},
            1 if namespace.len() == 33 => {},
            2 if namespace.len() == 65 => {},
            _ => return Err(ReplayWitnessError::Encoding),
        }
        let key = &address[2 + namespace_length..];
        if key.is_empty() || key.len() > crate::storage::MAX_STORAGE_KEY_BYTES || value.len() > crate::storage::MAX_STORAGE_VALUE_BYTES { return Err(ReplayWitnessError::Bounds); }
        let current = (namespace, key);
        if previous.is_some_and(|previous| previous >= current) { return Err(ReplayWitnessError::Encoding); }
        previous = Some(current);
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::abi::{Abi, AuthorizationContext, CapabilitySet, UnavailableReceiptOracle};
    use crate::host::RuntimeState;
    use crate::storage::{PrincipalId, ProgramId, Storage, StorageNamespace};

    fn state() -> RuntimeState {
        let program = ProgramId::new([1; 32]).unwrap_or_else(|error| panic!("program: {error}"));
        let principal = PrincipalId::new([2; 32]).unwrap_or_else(|error| panic!("principal: {error}"));
        let mut storage = Storage::new();
        for namespace in [StorageNamespace::shared(program), StorageNamespace::principal(program, principal), StorageNamespace::protocol_private(program, [3; 32])] {
            let mut transaction = storage.transaction(namespace);
            transaction.write(b"cell", b"actual storage value").unwrap_or_else(|error| panic!("write: {error}"));
            transaction.commit();
        }
        let authorization = AuthorizationContext::new(principal, CapabilitySet::empty());
        let abi = Abi::new(2, program, authorization, storage, &UnavailableReceiptOracle)
            .unwrap_or_else(|error| panic!("abi: {error}"));
        let mut meter = crate::Meter::declared();
        meter.charge_cpu(17).unwrap_or_else(|error| panic!("meter: {error}"));
        RuntimeState::sandbox(meter, abi)
    }

    #[test]
    fn real_host_preimages_and_meter_round_trip_without_charging_or_accepting_v2_side_metadata() {
        let state = state();
        let before = state.meter().replay_state_bytes().unwrap_or_else(|error| panic!("meter: {error:?}"));
        let maximum = crate::MAX_TRACE_STATE_BYTES as usize;
        let witness = state.replay_host_witness([4; 32], maximum).unwrap_or_else(|error| panic!("witness: {error:?}"));
        assert_eq!(state.meter().replay_state_bytes(), Ok(before));
        let root = state.v2_host_state_commitment().unwrap_or_else(|error| panic!("root: {error}"));
        let identity = state.v2_host_state_identity().unwrap_or_else(|error| panic!("identity: {error}"));
        assert_eq!(witness.compare_v2_preimages([4; 32], root.root, root.canonical_bytes, identity.base_state), Ok(()));
        let encoded = witness.canonical_bytes(maximum).unwrap_or_else(|error| panic!("encode: {error:?}"));
        assert_eq!(ReplayHostWitnessV1::decode(&encoded, encoded.len()), Ok(witness.clone()));
        assert_eq!(witness.canonical_bytes(encoded.len() - 1), Err(ReplayWitnessError::Bounds));
        let mut changed = witness.clone();
        let mut meter = changed.decode_untrusted_meter().unwrap_or_else(|error| panic!("meter decode: {error:?}"));
        meter.restore_cpu_carry(meter.cpu_carried() + 1);
        changed.meter = meter.replay_state_bytes().unwrap_or_else(|error| panic!("meter encode: {error:?}"));
        assert_eq!(changed.compare_v2_preimages([4; 32], root.root, root.canonical_bytes, identity.base_state), Ok(()));
        assert_ne!(changed.proposal_digest(maximum), witness.proposal_digest(maximum));
        assert_eq!(witness.compare_v2_preimages([5; 32], root.root, root.canonical_bytes, identity.base_state), Err(ReplayWitnessError::Binding));
        let mut changed = witness.clone();
        *changed.storage_baseline.last_mut().unwrap_or_else(|| panic!("baseline")) ^= 1;
        assert_eq!(changed.compare_v2_preimages([4; 32], root.root, root.canonical_bytes, identity.base_state), Err(ReplayWitnessError::Binding));
        let mut trailing = encoded.clone(); trailing.push(0);
        assert_eq!(ReplayHostWitnessV1::decode(&trailing, maximum), Err(ReplayWitnessError::Encoding));
        for length in 0..encoded.len() { assert!(ReplayHostWitnessV1::decode(&encoded[..length], maximum).is_err()); }
    }

    #[test]
    fn isolated_preimage_and_unavailable_or_oversized_capture_refuse_exactly() {
        let state = RuntimeState::isolated(crate::Meter::declared());
        let maximum = crate::MAX_TRACE_STATE_BYTES as usize;
        let witness = state.replay_host_witness([7; 32], maximum).unwrap_or_else(|error| panic!("isolated: {error:?}"));
        assert_eq!(witness.runtime, ISOLATED_DOMAIN);
        assert_eq!(witness.storage_baseline, STORAGE_DOMAIN);
        let encoded = witness.canonical_bytes(maximum).unwrap_or_else(|error| panic!("encode: {error:?}"));
        assert_eq!(ReplayHostWitnessV1::decode(&encoded, maximum), Ok(witness));
        assert_eq!(state.replay_host_witness([0; 32], maximum), Err(ReplayWitnessError::Binding));
        assert_eq!(state.replay_host_witness([7; 32], 1), Err(ReplayWitnessError::Bounds));
        assert_eq!(state.replay_host_witness([7; 32], maximum + 1), Err(ReplayWitnessError::Bounds));
    }
}

const STORAGE_PAIR_DOMAIN: &[u8] = b"LayerX/programs/replay-storage-witness/v1\0";

pub struct UntrustedStorageReplayPair {
    pub baseline: crate::storage::Storage,
    pub current: crate::storage::Storage,
    pub storage_overlay: Vec<(Vec<u8>, Option<Vec<u8>>)>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct StorageReplayWitnessV1 {
    code_hash: [u8; 32],
    baseline: Vec<u8>,
    current: Vec<u8>,
    overlay: Vec<u8>,
}

fn storage_overlay_bytes(entries: &[(Vec<u8>, Option<Vec<u8>>)], maximum: usize) -> Result<Vec<u8>, ReplayWitnessError> {
    let mut out = Vec::new();
    append(&mut out, &u32::try_from(entries.len()).map_err(|_| ReplayWitnessError::Bounds)?.to_be_bytes(), maximum)?;
    for (key, value) in entries {
        match value {
            Some(value) => { append(&mut out, &[0], maximum)?; field(&mut out, key, maximum)?; field(&mut out, value, maximum)?; }
            None => { append(&mut out, &[1], maximum)?; field(&mut out, key, maximum)?; }
        }
    }
    Ok(out)
}

impl StorageReplayWitnessV1 {
    pub(crate) fn capture(code_hash: [u8; 32], baseline: &crate::storage::Storage, current: &crate::storage::Storage, maximum: usize) -> Result<Self, ReplayWitnessError> {
        maximum_bytes(maximum)?;
        if code_hash == [0; 32] { return Err(ReplayWitnessError::Binding); }
        let mut remaining = maximum.checked_sub(STORAGE_PAIR_DOMAIN.len() + 32 + 12).ok_or(ReplayWitnessError::Bounds)?;
        let baseline_bytes = baseline.replay_state_bytes(remaining)?;
        remaining = remaining.checked_sub(baseline_bytes.len()).ok_or(ReplayWitnessError::Bounds)?;
        let current_bytes = current.replay_state_bytes(remaining)?;
        remaining = remaining.checked_sub(current_bytes.len()).ok_or(ReplayWitnessError::Bounds)?;
        let entries = current.bounded_replay_overlay(baseline, remaining)?;
        let overlay = storage_overlay_bytes(&entries, remaining)?;
        Ok(Self { code_hash, baseline: baseline_bytes, current: current_bytes, overlay })
    }
    fn encoded_len(&self) -> Result<usize, ReplayWitnessError> {
        (STORAGE_PAIR_DOMAIN.len() + 32 + 12).checked_add(self.baseline.len())
            .and_then(|n| n.checked_add(self.current.len())).and_then(|n| n.checked_add(self.overlay.len())).ok_or(ReplayWitnessError::Bounds)
    }
    pub fn canonical_bytes(&self, maximum: usize) -> Result<Vec<u8>, ReplayWitnessError> {
        maximum_bytes(maximum)?;
        if self.encoded_len()? > maximum { return Err(ReplayWitnessError::Bounds); }
        let mut out = Vec::new();
        append(&mut out, STORAGE_PAIR_DOMAIN, maximum)?;
        append(&mut out, &self.code_hash, maximum)?;
        field(&mut out, &self.baseline, maximum)?;
        field(&mut out, &self.current, maximum)?;
        field(&mut out, &self.overlay, maximum)?;
        Ok(out)
    }
    pub fn decode(bytes: &[u8], maximum: usize) -> Result<Self, ReplayWitnessError> {
        maximum_bytes(maximum)?;
        if bytes.len() > maximum { return Err(ReplayWitnessError::Bounds); }
        let mut cursor = ReplayCursor::new(bytes);
        if cursor.take(STORAGE_PAIR_DOMAIN.len())? != STORAGE_PAIR_DOMAIN { return Err(ReplayWitnessError::Encoding); }
        let code_hash = cursor.take(32)?.try_into().map_err(|_| ReplayWitnessError::Encoding)?;
        if code_hash == [0; 32] { return Err(ReplayWitnessError::Binding); }
        let baseline = cursor.field()?;
        let current = cursor.field()?;
        let overlay = cursor.field()?;
        if !cursor.done() { return Err(ReplayWitnessError::Encoding); }
        let restored_baseline = crate::storage::Storage::decode_untrusted_replay_state(baseline, maximum)?;
        let restored_current = crate::storage::Storage::decode_untrusted_replay_state(current, maximum)?;
        let actual_overlay = restored_current.bounded_replay_overlay(&restored_baseline, overlay.len())?;
        if storage_overlay_bytes(&actual_overlay, overlay.len())?.as_slice() != overlay { return Err(ReplayWitnessError::Binding); }
        Ok(Self { code_hash, baseline: copy(baseline)?, current: copy(current)?, overlay: copy(overlay)? })
    }
    pub fn decode_untrusted_storage_pair(&self, maximum: usize) -> Result<UntrustedStorageReplayPair, ReplayWitnessError> {
        maximum_bytes(maximum)?;
        if self.encoded_len()? > maximum { return Err(ReplayWitnessError::Bounds); }
        let baseline = crate::storage::Storage::decode_untrusted_replay_state(&self.baseline, maximum)?;
        let current = crate::storage::Storage::decode_untrusted_replay_state(&self.current, maximum)?;
        let storage_overlay = current.bounded_replay_overlay(&baseline, self.overlay.len())?;
        if storage_overlay_bytes(&storage_overlay, self.overlay.len())? != self.overlay { return Err(ReplayWitnessError::Binding); }
        Ok(UntrustedStorageReplayPair { baseline, current, storage_overlay })
    }
    pub fn proposal_digest(&self, maximum: usize) -> Result<[u8; 32], ReplayWitnessError> {
        Ok(Sha256::digest(self.canonical_bytes(maximum)?).into())
    }
    pub fn compare_host_witness_baseline(&self, host: &ReplayHostWitnessV1, maximum: usize) -> Result<(), ReplayWitnessError> {
        if self.code_hash != host.code_hash || host.abi.is_none() { return Err(ReplayWitnessError::Binding); }
        let pair = self.decode_untrusted_storage_pair(maximum)?;
        let mut legacy = Vec::new();
        append(&mut legacy, STORAGE_DOMAIN, host.storage_baseline.len())?;
        pair.baseline.try_for_each_commitment_entry(|key, value| {
            field(&mut legacy, &key, host.storage_baseline.len())?;
            field(&mut legacy, value, host.storage_baseline.len())
        })?;
        if legacy != host.storage_baseline { return Err(ReplayWitnessError::Binding); }
        Ok(())
    }
}

#[cfg(test)]
mod storage_pair_tests {
    use super::*;
    use crate::abi::{Abi, AuthorizationContext, Capability, CapabilitySet, UnavailableReceiptOracle};
    use crate::host::RuntimeState;
    use crate::storage::{PrincipalId, ProgramId, Storage, StorageNamespace};

    #[test]
    fn actual_abi_pair_restores_exact_current_baseline_and_observed_delta() {
        let maximum = crate::MAX_TRACE_STATE_BYTES as usize;
        let program = ProgramId::new([11; 32]).unwrap_or_else(|error| panic!("program: {error}"));
        let principal = PrincipalId::new([12; 32]).unwrap_or_else(|error| panic!("principal: {error}"));
        let namespace = StorageNamespace::principal(program, principal);
        let mut storage = Storage::new();
        storage.write(namespace, b"a", b"old").unwrap_or_else(|error| panic!("write: {error}"));
        storage.write(namespace, b"b", b"delete").unwrap_or_else(|error| panic!("write: {error}"));
        let baseline = storage.clone();
        let grants = CapabilitySet::new([Capability::StorageRead, Capability::StorageWrite]).unwrap_or_else(|error| panic!("capabilities: {error}"));
        let abi = Abi::new(2, program, AuthorizationContext::new(principal, grants), storage, &UnavailableReceiptOracle)
            .unwrap_or_else(|error| panic!("abi: {error}"));
        let mut state = RuntimeState::sandbox(crate::Meter::declared(), abi);
        state.with_abi(|abi, meter| {
            abi.storage_write(meter, b"a", b"new")?;
            abi.storage_delete(meter, b"b")?;
            abi.storage_write(meter, b"c", b"added")
        }).unwrap_or_else(|error| panic!("actual ABI mutation: {error}"));
        let witness = state.replay_storage_witness([13; 32], maximum).unwrap_or_else(|error| panic!("storage witness: {error:?}"));
        let host = state.replay_host_witness([13; 32], maximum).unwrap_or_else(|error| panic!("host witness: {error:?}"));
        assert_eq!(witness.compare_host_witness_baseline(&host, maximum), Ok(()));
        let restored = witness.decode_untrusted_storage_pair(maximum).unwrap_or_else(|error| panic!("restore: {error:?}"));
        assert_eq!(restored.baseline.replay_state_bytes(maximum), baseline.replay_state_bytes(maximum));
        let actual_abi = state.authorization_abi().unwrap_or_else(|| panic!("abi missing"));
        assert_eq!(restored.current.replay_state_bytes(maximum), actual_abi.storage_snapshot().replay_state_bytes(maximum));
        let mut observed = Vec::new();
        actual_abi.for_each_storage_commitment_delta(&baseline, |key, value| observed.push((key, value.map(<[u8]>::to_vec))));
        observed.sort_by(|left, right| left.0.cmp(&right.0));
        assert_eq!(restored.storage_overlay, observed);
        assert_eq!(observed.len(), 3);
        let encoded = witness.canonical_bytes(maximum).unwrap_or_else(|error| panic!("encode: {error:?}"));
        assert_eq!(StorageReplayWitnessV1::decode(&encoded, encoded.len()), Ok(witness.clone()));
        assert_eq!(witness.canonical_bytes(encoded.len() - 1), Err(ReplayWitnessError::Bounds));
        let mut forged = witness.clone();
        *forged.overlay.last_mut().unwrap_or_else(|| panic!("overlay missing")) ^= 1;
        let forged = forged.canonical_bytes(maximum).unwrap_or_else(|error| panic!("forge encoding: {error:?}"));
        assert!(StorageReplayWitnessV1::decode(&forged, maximum).is_err());
        let mut trailing = encoded.clone(); trailing.push(0);
        assert_eq!(StorageReplayWitnessV1::decode(&trailing, maximum), Err(ReplayWitnessError::Encoding));
        for length in 0..encoded.len() { assert!(StorageReplayWitnessV1::decode(&encoded[..length], maximum).is_err()); }
        let new_namespace = StorageNamespace::shared(program);
        assert_eq!(restored.current.read(new_namespace, b"missing"), Ok(None));
        let changed = StorageReplayWitnessV1::capture([13; 32], &restored.baseline, &restored.current, maximum)
            .unwrap_or_else(|error| panic!("access history: {error:?}"));
        assert_eq!(changed.overlay, witness.overlay);
        assert_eq!(changed.compare_host_witness_baseline(&host, maximum), Ok(()));
        assert_ne!(changed.proposal_digest(maximum), witness.proposal_digest(maximum));
    }

    #[test]
    fn absent_actual_storage_is_not_replaced_with_empty_storage() {
        let state = RuntimeState::isolated(crate::Meter::declared());
        assert_eq!(state.replay_storage_witness([14; 32], crate::MAX_TRACE_STATE_BYTES as usize), Err(ReplayWitnessError::StateUnavailable));
    }
}


const COMPOSITION_DOMAIN: &[u8] = b"LayerX/programs/replay-composition-witness/v1\0";

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CompositionReplayWitnessV1 {
    code_hash: [u8; 32],
    composition: Option<(crate::AbiRevision, Vec<u8>)>,
    failure_graph: Option<Vec<u8>>,
}

pub struct UntrustedCompositionReplayState {
    pub composition: Option<(crate::AbiRevision, crate::CallGraph)>,
    pub failure_graph: Option<crate::CallGraph>,
}

impl CompositionReplayWitnessV1 {
    pub(crate) fn capture(code_hash: [u8; 32], composition: Option<&crate::calls::Composition>, failure_graph: Option<&crate::CallGraph>, maximum: usize) -> Result<Self, ReplayWitnessError> {
        maximum_bytes(maximum)?;
        if code_hash == [0; 32] { return Err(ReplayWitnessError::Binding); }
        let overhead = COMPOSITION_DOMAIN.len() + 32 + 2
            + if composition.is_some() { 5 } else { 0 }
            + if failure_graph.is_some() { 4 } else { 0 };
        let mut remaining = maximum.checked_sub(overhead).ok_or(ReplayWitnessError::Bounds)?;
        let composition = composition.map(|composition| {
            let graph = composition.graph().replay_state_bytes(remaining)?;
            remaining = remaining.checked_sub(graph.len()).ok_or(ReplayWitnessError::Bounds)?;
            Ok::<_, ReplayWitnessError>((composition.revision(), graph))
        }).transpose()?;
        let failure_graph = failure_graph.map(|graph| graph.replay_state_bytes(remaining)).transpose()?;
        Ok(Self { code_hash, composition, failure_graph })
    }

    pub fn code_hash(&self) -> [u8; 32] { self.code_hash }

    pub fn canonical_bytes(&self, maximum: usize) -> Result<Vec<u8>, ReplayWitnessError> {
        maximum_bytes(maximum)?;
        let mut length = COMPOSITION_DOMAIN.len() + 32 + 2;
        if let Some((_, graph)) = &self.composition {
            length = length.checked_add(5).and_then(|n| n.checked_add(graph.len())).ok_or(ReplayWitnessError::Bounds)?;
        }
        if let Some(graph) = &self.failure_graph {
            length = length.checked_add(4).and_then(|n| n.checked_add(graph.len())).ok_or(ReplayWitnessError::Bounds)?;
        }
        if length > maximum { return Err(ReplayWitnessError::Bounds); }
        let mut out = Vec::new();
        out.try_reserve_exact(length).map_err(|_| ReplayWitnessError::Allocation)?;
        append(&mut out, COMPOSITION_DOMAIN, maximum)?;
        append(&mut out, &self.code_hash, maximum)?;
        match &self.composition {
            None => append(&mut out, &[0], maximum)?,
            Some((revision, graph)) => {
                let version = match revision { crate::AbiRevision::V1 => 1, crate::AbiRevision::V2 => 2, crate::AbiRevision::V3 => 3, crate::AbiRevision::V4 => 4 };
                append(&mut out, &[1, version], maximum)?;
                field(&mut out, graph, maximum)?;
            }
        }
        match &self.failure_graph {
            None => append(&mut out, &[0], maximum)?,
            Some(graph) => { append(&mut out, &[1], maximum)?; field(&mut out, graph, maximum)?; }
        }
        Ok(out)
    }

    pub fn decode(encoded: &[u8], maximum: usize) -> Result<Self, ReplayWitnessError> {
        maximum_bytes(maximum)?;
        if encoded.len() > maximum { return Err(ReplayWitnessError::Bounds); }
        let mut cursor = ReplayCursor::new(encoded);
        if cursor.take(COMPOSITION_DOMAIN.len())? != COMPOSITION_DOMAIN { return Err(ReplayWitnessError::Encoding); }
        let code_hash = cursor.take(32)?.try_into().map_err(|_| ReplayWitnessError::Encoding)?;
        if code_hash == [0; 32] { return Err(ReplayWitnessError::Binding); }
        let composition = match cursor.u8()? {
            0 => None,
            1 => {
                let revision = match cursor.u8()? { 1 => crate::AbiRevision::V1, 2 => crate::AbiRevision::V2, 3 => crate::AbiRevision::V3, 4 => crate::AbiRevision::V4, _ => return Err(ReplayWitnessError::Encoding) };
                let bytes = cursor.field()?;
                crate::CallGraph::decode_untrusted_replay_state(bytes, maximum)?;
                Some((revision, copy(bytes)?))
            }
            _ => return Err(ReplayWitnessError::Encoding),
        };
        let failure_graph = match cursor.u8()? {
            0 => None,
            1 => {
                let bytes = cursor.field()?;
                crate::CallGraph::decode_untrusted_replay_state(bytes, maximum)?;
                Some(copy(bytes)?)
            }
            _ => return Err(ReplayWitnessError::Encoding),
        };
        if !cursor.done() { return Err(ReplayWitnessError::Encoding); }
        Ok(Self { code_hash, composition, failure_graph })
    }

    pub fn decode_untrusted_state(&self, maximum: usize) -> Result<UntrustedCompositionReplayState, ReplayWitnessError> {
        self.canonical_bytes(maximum)?;
        Ok(UntrustedCompositionReplayState {
            composition: self.composition.as_ref().map(|(revision, graph)| {
                crate::CallGraph::decode_untrusted_replay_state(graph, maximum).map(|graph| (*revision, graph))
            }).transpose()?,
            failure_graph: self.failure_graph.as_ref().map(|graph| crate::CallGraph::decode_untrusted_replay_state(graph, maximum)).transpose()?,
        })
    }

    pub fn proposal_digest(&self, maximum: usize) -> Result<[u8; 32], ReplayWitnessError> {
        Ok(Sha256::digest(self.canonical_bytes(maximum)?).into())
    }
}


#[cfg(test)]
mod composition_replay_tests {
    use super::*;

    #[test]
    fn real_live_graph_restoration_retains_admission_history_and_refusals() {
        let root = crate::ProgramId::new([1; 32]).unwrap_or_else(|error| panic!("root: {error}"));
        let middle = crate::ProgramId::new([2; 32]).unwrap_or_else(|error| panic!("middle: {error}"));
        let leaf = crate::ProgramId::new([3; 32]).unwrap_or_else(|error| panic!("leaf: {error}"));
        let principal = crate::PrincipalId::new([4; 32]).unwrap_or_else(|error| panic!("principal: {error}"));
        let rules = crate::CompositionRules::new(2, 5, 2, 2).unwrap_or_else(|error| panic!("rules: {error}"));
        let mut graph = crate::CallGraph::root(rules, root, principal);
        let maximum = 4096;
        for (callee, leave) in [(middle, false), (leaf, true), (leaf, false)] {
            graph.enter(callee).unwrap_or_else(|error| panic!("enter: {error}"));
            let encoded = graph.replay_state_bytes(maximum).unwrap_or_else(|error| panic!("encode: {error:?}"));
            let restored = crate::CallGraph::decode_untrusted_replay_state(&encoded, maximum).unwrap_or_else(|error| panic!("restore: {error:?}"));
            assert_eq!(restored, graph);
            let mut actual_refusal = graph.clone();
            let mut restored_refusal = restored.clone();
            assert_eq!(actual_refusal.enter(root), restored_refusal.enter(root));
            assert_eq!(actual_refusal.enter(middle), restored_refusal.enter(middle));
            if leave { graph.leave(); }
        }
        graph.leave(); graph.leave();
        graph.enter(middle).unwrap_or_else(|error| panic!("repeat: {error}"));
        let composition = crate::calls::Composition::new(
            std::rc::Rc::new(crate::ProgramCatalog::new()), graph.clone(), crate::AbiRevision::V4,
        );
        let witness = CompositionReplayWitnessV1::capture([5; 32], Some(&composition), Some(&graph), maximum)
            .unwrap_or_else(|error| panic!("capture: {error:?}"));
        let encoded = witness.canonical_bytes(maximum).unwrap_or_else(|error| panic!("encode: {error:?}"));
        assert_eq!(CompositionReplayWitnessV1::decode(&encoded, maximum), Ok(witness.clone()));
        let restored = witness.decode_untrusted_state(maximum).unwrap_or_else(|error| panic!("decode: {error:?}"));
        assert_eq!(restored.composition, Some((crate::AbiRevision::V4, graph.clone())));
        assert_eq!(restored.failure_graph, Some(graph.clone()));
        let mut restored = crate::CallGraph::decode_untrusted_replay_state(
            &graph.replay_state_bytes(maximum).unwrap_or_else(|error| panic!("graph: {error:?}")), maximum,
        ).unwrap_or_else(|error| panic!("graph restore: {error:?}"));
        assert_eq!(restored.enter(leaf), graph.enter(leaf));
        assert_eq!(restored, graph);
    }

    #[test]
    fn graph_replay_refuses_forged_frames_visits_edges_and_bounds() {
        let root = crate::ProgramId::new([1; 32]).unwrap_or_else(|error| panic!("root: {error}"));
        let callee = crate::ProgramId::new([2; 32]).unwrap_or_else(|error| panic!("callee: {error}"));
        let principal = crate::PrincipalId::new([3; 32]).unwrap_or_else(|error| panic!("principal: {error}"));
        let mut graph = crate::CallGraph::root(crate::CompositionRules::declared(), root, principal);
        graph.enter(callee).unwrap_or_else(|error| panic!("enter: {error}"));
        let encoded = graph.replay_state_bytes(4096).unwrap_or_else(|error| panic!("encode: {error:?}"));
        assert_eq!(graph.replay_state_bytes(encoded.len() - 1), Err(ReplayWitnessError::Bounds));
        assert_eq!(crate::CallGraph::decode_untrusted_replay_state(&encoded, encoded.len() - 1), Err(ReplayWitnessError::Bounds));
        for length in 0..encoded.len() {
            assert!(crate::CallGraph::decode_untrusted_replay_state(&encoded[..length], 4096).is_err());
        }
        let mut forged = encoded.clone();
        let last = forged.len() - 1;
        forged[last] ^= 1;
        assert!(crate::CallGraph::decode_untrusted_replay_state(&forged, 4096).is_err());
        let edge_offset = b"LayerX/programs/replay-call-graph/v1\0".len() + 16 + 64 + 4;
        let mut forged = encoded.clone(); forged[edge_offset] = 1;
        assert!(crate::CallGraph::decode_untrusted_replay_state(&forged, 4096).is_err());
        let frame_offset = edge_offset + 118 + 4;
        let mut forged = encoded.clone(); forged[frame_offset] = 1;
        assert!(crate::CallGraph::decode_untrusted_replay_state(&forged, 4096).is_err());
        let mut surplus = encoded; surplus.push(0);
        assert!(crate::CallGraph::decode_untrusted_replay_state(&surplus, 4096).is_err());
        let isolated = CompositionReplayWitnessV1::capture([9; 32], None, None, 4096)
            .unwrap_or_else(|error| panic!("isolated: {error:?}"));
        let bytes = isolated.canonical_bytes(4096).unwrap_or_else(|error| panic!("isolated encode: {error:?}"));
        assert_eq!(CompositionReplayWitnessV1::decode(&bytes, 4096), Ok(isolated));
        assert_eq!(CompositionReplayWitnessV1::capture([0; 32], None, None, 4096), Err(ReplayWitnessError::Binding));
    }
}
