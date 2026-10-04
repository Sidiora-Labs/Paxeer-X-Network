use sha2::{Digest, Sha256};

use crate::replay::{append, ReplayWitnessError};

pub const PROGRAM_REPLAY_RECORD_DOMAIN: &[u8] = b"LXP/program-replay-record/v1\0";
pub const PROGRAM_REPLAY_WITNESS_DOMAIN: &[u8] = b"LXP/program-replay-witness/v1\0";
pub const MAX_PROGRAM_REPLAY_BYTES: u32 = 1024 * 1024;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct ProgramReplayProfile {
    maximum_boundaries: u32,
    maximum_bytes: u32,
    policy: crate::TracePolicy,
}

impl ProgramReplayProfile {
    pub fn new(maximum_boundaries: u32, maximum_bytes: u32) -> Result<Self, ReplayWitnessError> {
        if maximum_boundaries < 2 || maximum_bytes == 0 || maximum_bytes > MAX_PROGRAM_REPLAY_BYTES
        {
            return Err(ReplayWitnessError::Bounds);
        }
        let policy = crate::TracePolicy::new(1, maximum_boundaries)
            .map_err(|_| ReplayWitnessError::Bounds)?;
        Ok(Self {
            maximum_boundaries,
            maximum_bytes,
            policy,
        })
    }
    pub const fn maximum_boundaries(self) -> u32 {
        self.maximum_boundaries
    }
    pub const fn maximum_bytes(self) -> u32 {
        self.maximum_bytes
    }
    pub const fn trace_policy(self) -> crate::TracePolicy {
        self.policy
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ProgramReplayRecord {
    profile: ProgramReplayProfile,
    code_hash: [u8; 32],
    input_digest: [u8; 32],
    runtime_version: u16,
    abi_version: u16,
    fee_schedule_version: u32,
    metering_schedule_version: u32,
    terminal_status: u8,
    boundary_count: u32,
    boundary_root: [u8; 32],
    witness_digest: [u8; 32],
    bytes: Vec<u8>,
}

impl ProgramReplayRecord {
    #[allow(clippy::too_many_arguments)]
    pub(crate) fn from_captured(
        profile: ProgramReplayProfile,
        code_hash: [u8; 32],
        input_digest: [u8; 32],
        runtime_version: u16,
        abi_version: u16,
        fee_schedule_version: u32,
        metering_schedule_version: u32,
        terminal_status: u8,
        leaves: Vec<Vec<u8>>,
    ) -> Result<Self, ReplayWitnessError> {
        if terminal_status > 2
            || leaves.is_empty()
            || leaves.len() > profile.maximum_boundaries as usize
        {
            return Err(ReplayWitnessError::Bounds);
        }
        let maximum = profile.maximum_bytes as usize;
        let boundary_count = u32::try_from(leaves.len()).map_err(|_| ReplayWitnessError::Bounds)?;
        let mut witness = Vec::new();
        append(&mut witness, PROGRAM_REPLAY_WITNESS_DOMAIN, maximum)?;
        append(&mut witness, &boundary_count.to_be_bytes(), maximum)?;
        let mut hashes = Vec::new();
        hashes
            .try_reserve_exact(leaves.len())
            .map_err(|_| ReplayWitnessError::Allocation)?;
        for (index, leaf) in leaves.iter().enumerate() {
            let length = u32::try_from(leaf.len()).map_err(|_| ReplayWitnessError::Bounds)?;
            append(&mut witness, &length.to_be_bytes(), maximum)?;
            append(&mut witness, leaf, maximum)?;
            let mut hash = Sha256::new();
            hash.update(b"LXP/program-replay-leaf/v1\0");
            hash.update((index as u32).to_be_bytes());
            hash.update(length.to_be_bytes());
            hash.update(leaf);
            hashes.push(<[u8; 32]>::from(hash.finalize()));
        }
        while hashes.len() > 1 {
            let mut next = Vec::new();
            next.try_reserve_exact(hashes.len().div_ceil(2))
                .map_err(|_| ReplayWitnessError::Allocation)?;
            for pair in hashes.chunks(2) {
                let mut hash = Sha256::new();
                hash.update(b"LXP/program-replay-node/v1\0");
                hash.update(pair[0]);
                hash.update(*pair.get(1).unwrap_or(&pair[0]));
                next.push(<[u8; 32]>::from(hash.finalize()));
            }
            hashes = next;
        }
        let boundary_root = hashes[0];
        let witness_digest = <[u8; 32]>::from(Sha256::digest(&witness));
        let mut bytes = Vec::new();
        append(&mut bytes, PROGRAM_REPLAY_RECORD_DOMAIN, maximum)?;
        append(&mut bytes, &1u16.to_be_bytes(), maximum)?;
        append(&mut bytes, &code_hash, maximum)?;
        append(&mut bytes, &input_digest, maximum)?;
        append(&mut bytes, &runtime_version.to_be_bytes(), maximum)?;
        append(&mut bytes, &abi_version.to_be_bytes(), maximum)?;
        append(&mut bytes, &fee_schedule_version.to_be_bytes(), maximum)?;
        append(
            &mut bytes,
            &metering_schedule_version.to_be_bytes(),
            maximum,
        )?;
        append(
            &mut bytes,
            &profile.maximum_boundaries.to_be_bytes(),
            maximum,
        )?;
        append(&mut bytes, &profile.maximum_bytes.to_be_bytes(), maximum)?;
        append(&mut bytes, &[terminal_status], maximum)?;
        append(&mut bytes, &boundary_count.to_be_bytes(), maximum)?;
        append(&mut bytes, &boundary_root, maximum)?;
        append(&mut bytes, &witness_digest, maximum)?;
        append(
            &mut bytes,
            &u32::try_from(witness.len())
                .map_err(|_| ReplayWitnessError::Bounds)?
                .to_be_bytes(),
            maximum,
        )?;
        append(&mut bytes, &witness, maximum)?;
        Ok(Self {
            profile,
            code_hash,
            input_digest,
            runtime_version,
            abi_version,
            fee_schedule_version,
            metering_schedule_version,
            terminal_status,
            boundary_count,
            boundary_root,
            witness_digest,
            bytes,
        })
    }
    pub const fn profile(&self) -> ProgramReplayProfile {
        self.profile
    }
    pub const fn code_hash(&self) -> [u8; 32] {
        self.code_hash
    }
    pub const fn input_digest(&self) -> [u8; 32] {
        self.input_digest
    }
    pub const fn runtime_version(&self) -> u16 {
        self.runtime_version
    }
    pub const fn abi_version(&self) -> u16 {
        self.abi_version
    }
    pub const fn fee_schedule_version(&self) -> u32 {
        self.fee_schedule_version
    }
    pub const fn metering_schedule_version(&self) -> u32 {
        self.metering_schedule_version
    }
    pub const fn terminal_status(&self) -> u8 {
        self.terminal_status
    }
    pub const fn boundary_count(&self) -> u32 {
        self.boundary_count
    }
    pub const fn boundary_root(&self) -> [u8; 32] {
        self.boundary_root
    }
    pub const fn witness_digest(&self) -> [u8; 32] {
        self.witness_digest
    }
    pub fn canonical_bytes(&self) -> &[u8] {
        &self.bytes
    }
}

pub(crate) fn captured_leaf(
    replay: &wasmi::ExecutionReplaySnapshot,
    arbitration: &crate::ArbitrationExecutionState,
    semantic: &crate::replay::CapturedSemanticReplayV2,
    maximum: usize,
) -> Result<Vec<u8>, ReplayWitnessError> {
    fn field(bytes: &mut Vec<u8>, value: &[u8], maximum: usize) -> Result<(), ReplayWitnessError> {
        append(
            bytes,
            &u32::try_from(value.len())
                .map_err(|_| ReplayWitnessError::Bounds)?
                .to_be_bytes(),
            maximum,
        )?;
        append(bytes, value, maximum)
    }
    let mut bytes = Vec::new();
    append(&mut bytes, b"LXP/program-replay-boundary/v1\0", maximum)?;
    let state = arbitration
        .canonical_bytes()
        .map_err(|_| ReplayWitnessError::Binding)?;
    field(&mut bytes, &portable_state_bytes(&state, maximum)?, maximum)?;
    append(
        &mut bytes,
        &u32::try_from(replay.frames.len())
            .map_err(|_| ReplayWitnessError::Bounds)?
            .to_be_bytes(),
        maximum,
    )?;
    for frame in &replay.frames {
        append(
            &mut bytes,
            &frame.module_function_index.to_be_bytes(),
            maximum,
        )?;
        append(&mut bytes, &frame.instruction_offset.to_be_bytes(), maximum)?;
        append(&mut bytes, &frame.value_base.to_be_bytes(), maximum)?;
        append(
            &mut bytes,
            &u32::try_from(frame.operand_types.len())
                .map_err(|_| ReplayWitnessError::Bounds)?
                .to_be_bytes(),
            maximum,
        )?;
        for value_type in &frame.operand_types {
            append(
                &mut bytes,
                &[match value_type {
                    wasmi::ExecutionValueType::I32 => 0,
                    wasmi::ExecutionValueType::I64 => 1,
                }],
                maximum,
            )?;
        }
    }
    field(&mut bytes, &replay.snapshot.canonical_instruction, maximum)?;
    append(
        &mut bytes,
        &replay.snapshot.instruction_fuel.to_be_bytes(),
        maximum,
    )?;
    append(
        &mut bytes,
        &replay.snapshot.memory_expansion_bytes.to_be_bytes(),
        maximum,
    )?;
    let supplement = &replay.snapshot.supplement;
    for value in [
        supplement.canonical_state_bytes,
        supplement.commitment_fuel,
        supplement.arbitration_engine_canonical_bytes,
        supplement.arbitration_instance_retained_bytes,
        supplement.arbitration_canonical_state_bytes,
        supplement.arbitration_commitment_fuel,
    ] {
        append(&mut bytes, &value.to_be_bytes(), maximum)?;
    }
    field(&mut bytes, &semantic.canonical_bytes(maximum)?, maximum)?;
    Ok(bytes)
}

fn portable_state_bytes(state: &[u8], maximum: usize) -> Result<Vec<u8>, ReplayWitnessError> {
    let mut bytes = Vec::new();
    append(&mut bytes, &[1], maximum)?;
    append(
        &mut bytes,
        &u32::try_from(state.len())
            .map_err(|_| ReplayWitnessError::Bounds)?
            .to_be_bytes(),
        maximum,
    )?;
    let mut offset = 0;
    let mut literal_start = 0;
    while offset < state.len() {
        if state[offset] != 0 {
            offset += 1;
            continue;
        }
        let zero_start = offset;
        while offset < state.len() && state[offset] == 0 {
            offset += 1;
        }
        if offset - zero_start < 8 {
            continue;
        }
        if zero_start > literal_start {
            append(&mut bytes, &[1], maximum)?;
            append(
                &mut bytes,
                &u32::try_from(zero_start - literal_start)
                    .map_err(|_| ReplayWitnessError::Bounds)?
                    .to_be_bytes(),
                maximum,
            )?;
            append(&mut bytes, &state[literal_start..zero_start], maximum)?;
        }
        append(&mut bytes, &[0], maximum)?;
        append(
            &mut bytes,
            &u32::try_from(offset - zero_start)
                .map_err(|_| ReplayWitnessError::Bounds)?
                .to_be_bytes(),
            maximum,
        )?;
        literal_start = offset;
    }
    if literal_start < state.len() {
        append(&mut bytes, &[1], maximum)?;
        append(
            &mut bytes,
            &u32::try_from(state.len() - literal_start)
                .map_err(|_| ReplayWitnessError::Bounds)?
                .to_be_bytes(),
            maximum,
        )?;
        append(&mut bytes, &state[literal_start..], maximum)?;
    }
    Ok(bytes)
}

pub fn decode_portable_state_bytes_untrusted(
    bytes: &[u8],
    maximum_decoded_bytes: usize,
) -> Result<Vec<u8>, ReplayWitnessError> {
    use crate::replay::ReplayCursor;
    if bytes.len() > MAX_PROGRAM_REPLAY_BYTES as usize
        || maximum_decoded_bytes == 0
        || maximum_decoded_bytes > crate::MAX_ARBITRATION_STATE_BYTES
    {
        return Err(ReplayWitnessError::Bounds);
    }
    let mut cursor = ReplayCursor::new(bytes);
    if cursor.u8()? != 1 {
        return Err(ReplayWitnessError::Encoding);
    }
    let length = cursor.u32()? as usize;
    if length > maximum_decoded_bytes {
        return Err(ReplayWitnessError::Bounds);
    }
    let mut decoded = Vec::new();
    decoded
        .try_reserve_exact(length)
        .map_err(|_| ReplayWitnessError::Allocation)?;
    while decoded.len() < length {
        let tag = cursor.u8()?;
        let count = cursor.u32()? as usize;
        if count == 0 || count > length - decoded.len() {
            return Err(ReplayWitnessError::Bounds);
        }
        match tag {
            0 => decoded.resize(decoded.len() + count, 0),
            1 => decoded.extend_from_slice(cursor.take(count)?),
            _ => return Err(ReplayWitnessError::Encoding),
        }
    }
    if !cursor.done() {
        return Err(ReplayWitnessError::Encoding);
    }
    if portable_state_bytes(&decoded, MAX_PROGRAM_REPLAY_BYTES as usize)? != bytes {
        return Err(ReplayWitnessError::Encoding);
    }
    Ok(decoded)
}
