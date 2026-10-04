use std::sync::Arc;

use sha2::{Digest, Sha256};

use crate::replay::{append, ReplayCursor, ReplayWitnessError};
use crate::replay_record::{
    ProgramReplayProfile, ProgramReplayRecord, MAX_PROGRAM_REPLAY_BYTES,
    PROGRAM_REPLAY_RECORD_DOMAIN, PROGRAM_REPLAY_WITNESS_DOMAIN,
};

#[derive(Debug, Clone)]
pub struct PortableTrap {
    pub code: Option<wasmi::core::TrapCode>,
    pub host_trap: bool,
}

#[derive(Debug, Clone)]
pub struct PortableBoundary {
    pub replay: wasmi::ExecutionReplaySnapshot,
    pub arbitration: crate::ArbitrationExecutionState,
    pub semantic_bytes: Vec<u8>,
    pub trap: Option<PortableTrap>,
}

fn bounded(bytes: &[u8], maximum: usize) -> Result<(), ReplayWitnessError> {
    if maximum == 0 || maximum > MAX_PROGRAM_REPLAY_BYTES as usize || bytes.len() > maximum {
        return Err(ReplayWitnessError::Bounds);
    }
    Ok(())
}

fn reserve<T>(count: usize) -> Result<Vec<T>, ReplayWitnessError> {
    let mut result = Vec::new();
    result
        .try_reserve_exact(count)
        .map_err(|_| ReplayWitnessError::Allocation)?;
    Ok(result)
}

fn copy(bytes: &[u8]) -> Result<Vec<u8>, ReplayWitnessError> {
    let mut result = reserve(bytes.len())?;
    result.extend_from_slice(bytes);
    Ok(result)
}

fn value(cursor: &mut ReplayCursor<'_>) -> Result<crate::ExecutionValue, ReplayWitnessError> {
    match cursor.u8()? {
        0 => Ok(crate::ExecutionValue::I32(cursor.i32()?)),
        1 => Ok(crate::ExecutionValue::I64(i64::from_be_bytes(
            cursor.array()?,
        ))),
        _ => Err(ReplayWitnessError::Encoding),
    }
}

fn values(cursor: &mut ReplayCursor<'_>) -> Result<Vec<crate::ExecutionValue>, ReplayWitnessError> {
    let count = cursor.count(5)?;
    let mut result = reserve(count)?;
    for _ in 0..count {
        result.push(value(cursor)?);
    }
    Ok(result)
}

fn wasmi_value(value: crate::ExecutionValue) -> wasmi::ExecutionTraceValue {
    match value {
        crate::ExecutionValue::I32(value) => wasmi::ExecutionTraceValue {
            value_type: wasmi::ExecutionValueType::I32,
            bits: u64::from(u32::from_be_bytes(value.to_be_bytes())),
        },
        crate::ExecutionValue::I64(value) => wasmi::ExecutionTraceValue {
            value_type: wasmi::ExecutionValueType::I64,
            bits: u64::from_be_bytes(value.to_be_bytes()),
        },
    }
}

fn wasmi_values(
    values: &[crate::ExecutionValue],
) -> Result<Vec<wasmi::ExecutionTraceValue>, ReplayWitnessError> {
    let mut result = reserve(values.len())?;
    result.extend(values.iter().copied().map(wasmi_value));
    Ok(result)
}

fn legacy(bytes: &[u8]) -> Result<crate::ExecutionState, ReplayWitnessError> {
    if bytes.len() > crate::MAX_STEP_STATE_BYTES {
        return Err(ReplayWitnessError::Bounds);
    }
    let mut cursor = ReplayCursor::new(bytes);
    if cursor.u16()? != crate::STEP_COMMITMENT_VERSION {
        return Err(ReplayWitnessError::Encoding);
    }
    let module_code_hash = cursor.array()?;
    let input_digest = cursor.array()?;
    let execution_parameters_digest = cursor.array()?;
    let step_index = cursor.u64()?;
    let program_counter = cursor.u64()?;
    let value_stack = values(&mut cursor)?;
    let count = cursor.count(9)?;
    let mut call_frames = reserve(count)?;
    for _ in 0..count {
        let function_index = cursor.u32()?;
        let return_program_counter = if cursor.boolean()? {
            Some(cursor.u64()?)
        } else {
            None
        };
        call_frames.push(crate::ExecutionFrame {
            function_index,
            return_program_counter,
            locals: values(&mut cursor)?,
        });
    }
    let count = cursor.count(6)?;
    let mut control_stack = reserve(count)?;
    for _ in 0..count {
        let kind = cursor.u8()?;
        if kind > 3 {
            return Err(ReplayWitnessError::Encoding);
        }
        control_stack.push(crate::ExecutionControlFrame {
            kind,
            operand_stack_height: cursor.u32()?,
            unreachable: cursor.boolean()?,
        });
    }
    let linear_memory = cursor.owned_field(crate::MAX_STEP_STATE_BYTES)?;
    let count = cursor.count(10)?;
    let mut globals = reserve(count)?;
    for _ in 0..count {
        globals.push(crate::ExecutionGlobal {
            global_index: cursor.u32()?,
            mutable: cursor.boolean()?,
            value: value(&mut cursor)?,
        });
    }
    let count = cursor.count(5)?;
    let mut storage_overlay = reserve(count)?;
    for _ in 0..count {
        storage_overlay.push(match cursor.u8()? {
            0 => crate::StorageOverlayEntry::Write {
                key: cursor.owned_field(crate::MAX_STEP_STATE_BYTES)?,
                value: cursor.owned_field(crate::MAX_STEP_STATE_BYTES)?,
            },
            1 => crate::StorageOverlayEntry::Delete {
                key: cursor.owned_field(crate::MAX_STEP_STATE_BYTES)?,
            },
            _ => return Err(ReplayWitnessError::Encoding),
        });
    }
    let fuel_remaining = cursor.u64()?;
    let metered_usage = crate::MeteredUsage {
        cpu_fuel: cursor.u64()?,
        memory_bytes: cursor.u64()?,
        storage_read_bytes: cursor.u64()?,
        storage_write_bytes: cursor.u64()?,
        output_values: cursor.u32()?,
        output_bytes: cursor.u64()?,
        occupancy_byte_batches: cursor.u128()?,
        occupancy_fee_units: cursor.u128()?,
        fee_units: cursor.u128()?,
    };
    if !cursor.done() {
        return Err(ReplayWitnessError::Encoding);
    }
    let result = crate::ExecutionState {
        module_code_hash,
        input_digest,
        execution_parameters_digest,
        step_index,
        program_counter,
        value_stack,
        call_frames,
        control_stack,
        linear_memory,
        globals,
        storage_overlay,
        fuel_remaining,
        metered_usage,
    };
    if result
        .canonical_bytes()
        .map_err(|_| ReplayWitnessError::Binding)?
        != bytes
    {
        return Err(ReplayWitnessError::Encoding);
    }
    Ok(result)
}

fn arbitration(bytes: &[u8]) -> Result<crate::ArbitrationExecutionState, ReplayWitnessError> {
    let mut cursor = ReplayCursor::new(bytes);
    if cursor.u16()? != crate::ARBITRATION_STEP_COMMITMENT_VERSION {
        return Err(ReplayWitnessError::Encoding);
    }
    let runtime_version = cursor.u16()?;
    let abi_version = cursor.u16()?;
    let fee_schedule_version = cursor.u32()?;
    let metering_schedule_version = cursor.u32()?;
    let trace_policy = crate::TracePolicy::new(cursor.u64()?, cursor.u32()?)
        .map_err(|_| ReplayWitnessError::Bounds)?;
    let module_code_hash = cursor.array()?;
    let input_digest = cursor.array()?;
    let execution_parameters_digest: [u8; 32] = cursor.array()?;
    let host_base_state_root = cursor.array()?;
    let receipt_oracle_root = cursor.array()?;
    let balance_oracle_root = cursor.array()?;
    let legacy = Arc::new(legacy(cursor.field()?)?);
    if legacy.execution_parameters_digest != execution_parameters_digest {
        return Err(ReplayWitnessError::Binding);
    }
    let engine_state = cursor.owned_field(crate::MAX_ARBITRATION_ENGINE_STATE_BYTES)?;
    let host_state_root = cursor.array()?;
    let host_state_bytes = cursor.u64()?;
    if !cursor.done() {
        return Err(ReplayWitnessError::Encoding);
    }
    let result = crate::ArbitrationExecutionState {
        identity: crate::ArbitrationExecutionIdentity {
            module_code_hash,
            input_digest,
            runtime_version,
            abi_version,
            fee_schedule_version,
            metering_schedule_version,
            trace_policy,
            host_base_state_root,
            receipt_oracle_root,
            balance_oracle_root,
        },
        legacy,
        engine_state,
        host_state_root,
        host_state_bytes,
    };
    if result
        .canonical_bytes()
        .map_err(|_| ReplayWitnessError::Binding)?
        != bytes
    {
        return Err(ReplayWitnessError::Encoding);
    }
    Ok(result)
}

fn optional_u32(cursor: &mut ReplayCursor<'_>) -> Result<Option<u32>, ReplayWitnessError> {
    Ok(if cursor.boolean()? {
        Some(cursor.u32()?)
    } else {
        None
    })
}

fn function_refs(
    cursor: &mut ReplayCursor<'_>,
) -> Result<Vec<Option<wasmi::ExecutionFunctionRef>>, ReplayWitnessError> {
    let count = cursor.count(1)?;
    let mut result = reserve(count)?;
    for _ in 0..count {
        result.push(if cursor.boolean()? {
            Some(wasmi::ExecutionFunctionRef {
                instance_index: cursor.u32()?,
                function_index: cursor.u32()?,
            })
        } else {
            None
        });
    }
    Ok(result)
}

fn instances(bytes: &[u8]) -> Result<Vec<wasmi::ExecutionInstanceState>, ReplayWitnessError> {
    let mut cursor = ReplayCursor::new(bytes);
    let count = cursor.count(24)?;
    let mut result = reserve(count)?;
    for _ in 0..count {
        let instance_index = cursor.u32()?;
        let count = cursor.count(13)?;
        let mut memories = reserve(count)?;
        for _ in 0..count {
            memories.push(wasmi::ExecutionMemory {
                memory_index: cursor.u32()?,
                initial_pages: cursor.u32()?,
                maximum_pages: optional_u32(&mut cursor)?,
                bytes: cursor.owned_field(crate::MAX_ARBITRATION_ENGINE_STATE_BYTES)?,
            });
        }
        let count = cursor.count(10)?;
        let mut globals = reserve(count)?;
        for _ in 0..count {
            globals.push(wasmi::ExecutionTraceGlobal {
                global_index: cursor.u32()?,
                mutable: cursor.boolean()?,
                value: wasmi_value(value(&mut cursor)?),
            });
        }
        let count = cursor.count(13)?;
        let mut tables = reserve(count)?;
        for _ in 0..count {
            tables.push(wasmi::ExecutionTable {
                table_index: cursor.u32()?,
                minimum: cursor.u32()?,
                maximum: optional_u32(&mut cursor)?,
                elements: function_refs(&mut cursor)?,
            });
        }
        let count = cursor.count(9)?;
        let mut data_segments = reserve(count)?;
        for _ in 0..count {
            data_segments.push(wasmi::ExecutionDataSegment {
                segment_index: cursor.u32()?,
                dropped: cursor.boolean()?,
                bytes: cursor.owned_field(crate::MAX_ARBITRATION_ENGINE_STATE_BYTES)?,
            });
        }
        let count = cursor.count(9)?;
        let mut element_segments = reserve(count)?;
        for _ in 0..count {
            element_segments.push(wasmi::ExecutionElementSegment {
                segment_index: cursor.u32()?,
                dropped: cursor.boolean()?,
                elements: function_refs(&mut cursor)?,
            });
        }
        result.push(wasmi::ExecutionInstanceState {
            instance_index,
            memories,
            globals,
            tables,
            data_segments,
            element_segments,
        });
    }
    if !cursor.done() {
        return Err(ReplayWitnessError::Encoding);
    }
    Ok(result)
}

fn trap_code(tag: u8) -> Result<Option<wasmi::core::TrapCode>, ReplayWitnessError> {
    use wasmi::core::TrapCode;
    Ok(match tag {
        0 => None,
        1 => Some(TrapCode::UnreachableCodeReached),
        2 => Some(TrapCode::MemoryOutOfBounds),
        3 => Some(TrapCode::TableOutOfBounds),
        4 => Some(TrapCode::IndirectCallToNull),
        5 => Some(TrapCode::IntegerDivisionByZero),
        6 => Some(TrapCode::IntegerOverflow),
        7 => Some(TrapCode::BadConversionToInteger),
        8 => Some(TrapCode::StackOverflow),
        9 => Some(TrapCode::BadSignature),
        10 => Some(TrapCode::OutOfFuel),
        11 => Some(TrapCode::GrowthOperationLimited),
        _ => return Err(ReplayWitnessError::Encoding),
    })
}

fn trap_tag(code: &Option<wasmi::core::TrapCode>) -> u8 {
    use wasmi::core::TrapCode;
    match code {
        None => 0,
        Some(TrapCode::UnreachableCodeReached) => 1,
        Some(TrapCode::MemoryOutOfBounds) => 2,
        Some(TrapCode::TableOutOfBounds) => 3,
        Some(TrapCode::IndirectCallToNull) => 4,
        Some(TrapCode::IntegerDivisionByZero) => 5,
        Some(TrapCode::IntegerOverflow) => 6,
        Some(TrapCode::BadConversionToInteger) => 7,
        Some(TrapCode::StackOverflow) => 8,
        Some(TrapCode::BadSignature) => 9,
        Some(TrapCode::OutOfFuel) => 10,
        Some(TrapCode::GrowthOperationLimited) => 11,
    }
}

impl PortableBoundary {
    pub fn decode_untrusted(bytes: &[u8], maximum: usize) -> Result<Self, ReplayWitnessError> {
        bounded(bytes, maximum)?;
        let mut cursor = ReplayCursor::new(bytes);
        let domain = b"LXP/program-replay-boundary/v1\0";
        if cursor.take(domain.len())? != domain {
            return Err(ReplayWitnessError::Encoding);
        }
        let decoded = crate::replay_record::decode_portable_state_bytes_untrusted(
            cursor.field()?,
            crate::MAX_ARBITRATION_STATE_BYTES,
        )?;
        let arbitration = arbitration(&decoded)?;
        let count = cursor.count(16)?;
        let mut frames = reserve(count)?;
        for _ in 0..count {
            let module_function_index = cursor.u32()?;
            let instruction_offset = cursor.u32()?;
            let value_base = cursor.u32()?;
            let count = cursor.count(1)?;
            let mut operand_types = reserve(count)?;
            for _ in 0..count {
                operand_types.push(match cursor.u8()? {
                    0 => wasmi::ExecutionValueType::I32,
                    1 => wasmi::ExecutionValueType::I64,
                    _ => return Err(ReplayWitnessError::Encoding),
                });
            }
            frames.push(wasmi::ExecutionReplayFrame {
                module_function_index,
                instruction_offset,
                value_base,
                operand_types,
            });
        }
        let canonical_instruction = cursor.owned_field(crate::MAX_STEP_INSTRUCTION_BYTES)?;
        let instruction_fuel = cursor.u64()?;
        let memory_expansion_bytes = cursor.u64()?;
        let canonical_state_bytes = cursor.u64()?;
        let commitment_fuel = cursor.u64()?;
        let arbitration_engine_canonical_bytes = cursor.u64()?;
        let arbitration_instance_retained_bytes = cursor.u64()?;
        let arbitration_canonical_state_bytes = cursor.u64()?;
        let arbitration_commitment_fuel = cursor.u64()?;
        let semantic_bytes = cursor.owned_field(maximum)?;
        let trap = match cursor.u8()? {
            0 => None,
            1 => Some(PortableTrap {
                code: trap_code(cursor.u8()?)?,
                host_trap: cursor.boolean()?,
            }),
            _ => return Err(ReplayWitnessError::Encoding),
        };
        if !cursor.done() {
            return Err(ReplayWitnessError::Encoding);
        }
        let legacy = &arbitration.legacy;
        let usage = legacy.metered_usage;
        let mut control_stack = reserve(legacy.control_stack.len())?;
        for frame in &legacy.control_stack {
            control_stack.push(wasmi::ExecutionControlFrame {
                kind: match frame.kind {
                    0 => wasmi::ExecutionControlKind::Block,
                    1 => wasmi::ExecutionControlKind::If,
                    2 => wasmi::ExecutionControlKind::Else,
                    3 => wasmi::ExecutionControlKind::Loop,
                    _ => return Err(ReplayWitnessError::Encoding),
                },
                operand_stack_height: frame.operand_stack_height,
                unreachable: frame.unreachable,
            });
        }
        let mut call_frames = reserve(legacy.call_frames.len())?;
        for frame in &legacy.call_frames {
            call_frames.push(wasmi::ExecutionTraceFrame {
                function_index: frame.function_index,
                return_program_counter: frame.return_program_counter,
                locals: wasmi_values(&frame.locals)?,
            });
        }
        let mut globals = reserve(legacy.globals.len())?;
        for global in &legacy.globals {
            globals.push(wasmi::ExecutionTraceGlobal {
                global_index: global.global_index,
                mutable: global.mutable,
                value: wasmi_value(global.value),
            });
        }
        let mut storage_overlay = reserve(legacy.storage_overlay.len())?;
        for entry in &legacy.storage_overlay {
            storage_overlay.push(match entry {
                crate::StorageOverlayEntry::Write { key, value } => {
                    (copy(key)?, Some(copy(value)?))
                }
                crate::StorageOverlayEntry::Delete { key } => (copy(key)?, None),
            });
        }
        let snapshot = wasmi::ExecutionSnapshot {
            step_index: legacy.step_index,
            program_counter: legacy.program_counter,
            value_stack: wasmi_values(&legacy.value_stack)?,
            call_frames,
            linear_memory: copy(&legacy.linear_memory)?,
            globals,
            arbitration_instances: instances(&arbitration.engine_state)?,
            control_stack,
            canonical_instruction,
            instruction_fuel,
            memory_expansion_bytes,
            supplement: wasmi::ExecutionSupplement {
                storage_overlay,
                authoritative_fuel: legacy.fuel_remaining,
                authoritative_usage: wasmi::ExecutionMeteredUsage {
                    cpu_fuel: usage.cpu_fuel,
                    memory_bytes: usage.memory_bytes,
                    storage_read_bytes: usage.storage_read_bytes,
                    storage_write_bytes: usage.storage_write_bytes,
                    output_values: usage.output_values,
                    output_bytes: usage.output_bytes,
                    occupancy_byte_batches: usage.occupancy_byte_batches,
                    occupancy_fee_units: usage.occupancy_fee_units,
                    fee_units: usage.fee_units,
                },
                canonical_state_bytes,
                commitment_fuel,
                arbitration_host_state_root: arbitration.host_state_root,
                arbitration_host_state_bytes: arbitration.host_state_bytes,
                arbitration_base_state_root: arbitration.identity.host_base_state_root,
                arbitration_receipt_oracle_root: arbitration.identity.receipt_oracle_root,
                arbitration_balance_oracle_root: arbitration.identity.balance_oracle_root,
                arbitration_engine_canonical_bytes,
                arbitration_instance_retained_bytes,
                arbitration_canonical_state_bytes,
                arbitration_commitment_fuel,
            },
        };
        if crate::execute::arbitration_engine_state_bytes(&snapshot)
            .map_err(|_| ReplayWitnessError::Binding)?
            != arbitration.engine_state
        {
            return Err(ReplayWitnessError::Encoding);
        }
        let result = Self {
            replay: wasmi::ExecutionReplaySnapshot {
                snapshot: Arc::new(snapshot),
                frames,
            },
            arbitration,
            semantic_bytes,
            trap,
        };
        if result.reencode_untrusted(maximum)? != bytes {
            return Err(ReplayWitnessError::Encoding);
        }
        Ok(result)
    }

    pub fn reencode_untrusted(&self, maximum: usize) -> Result<Vec<u8>, ReplayWitnessError> {
        bounded(&[], maximum)?;
        let mut bytes = crate::replay_record::captured_leaf_bytes(
            &self.replay,
            &self.arbitration,
            &self.semantic_bytes,
            maximum,
        )?;
        match &self.trap {
            None => append(&mut bytes, &[0], maximum)?,
            Some(trap) => append(
                &mut bytes,
                &[1, trap_tag(&trap.code), u8::from(trap.host_trap)],
                maximum,
            )?,
        }
        Ok(bytes)
    }
}

#[derive(Debug, Clone)]
pub struct PortableReplayRecord {
    pub record: ProgramReplayRecord,
    pub authority_bytes: Vec<u8>,
    pub hosts_bytes: Vec<u8>,
    pub leaves: Vec<Vec<u8>>,
}

impl PortableReplayRecord {
    pub fn decode_untrusted(bytes: &[u8], maximum: usize) -> Result<Self, ReplayWitnessError> {
        bounded(bytes, maximum)?;
        let mut cursor = ReplayCursor::new(bytes);
        let domain = b"LXP/program-replay-native-blob/v1\0";
        if cursor.take(domain.len())? != domain {
            return Err(ReplayWitnessError::Encoding);
        }
        let runtime = cursor.field()?;
        let authority_bytes = cursor.owned_field(maximum)?;
        let hosts_bytes = cursor.owned_field(maximum)?;
        if !cursor.done()
            || !authority_bytes.starts_with(b"LXP/program-replay-authority/v1\0")
            || !hosts_bytes.starts_with(b"LXP/program-replay-hosts/v1\0")
        {
            return Err(ReplayWitnessError::Encoding);
        }
        let (record, leaves) = decode_record(runtime, maximum)?;
        if bytes.len() > record.profile().maximum_bytes() as usize {
            return Err(ReplayWitnessError::Bounds);
        }
        Ok(Self {
            record,
            authority_bytes,
            hosts_bytes,
            leaves,
        })
    }

    pub fn merkle_path_untrusted(&self, index: u32) -> Result<Vec<[u8; 32]>, ReplayWitnessError> {
        let mut position = usize::try_from(index).map_err(|_| ReplayWitnessError::Bounds)?;
        if position >= self.leaves.len() {
            return Err(ReplayWitnessError::Bounds);
        }
        let mut hashes = reserve(self.leaves.len())?;
        for (index, leaf) in self.leaves.iter().enumerate() {
            hashes.push(replay_leaf_hash(
                u32::try_from(index).map_err(|_| ReplayWitnessError::Bounds)?,
                leaf,
            )?);
        }
        let mut path = Vec::new();
        while hashes.len() > 1 {
            let sibling = if position % 2 == 0 {
                (position + 1).min(hashes.len() - 1)
            } else {
                position - 1
            };
            path.try_reserve(1)
                .map_err(|_| ReplayWitnessError::Allocation)?;
            path.push(hashes[sibling]);
            let mut next = reserve(hashes.len().div_ceil(2))?;
            for pair in hashes.chunks(2) {
                next.push(replay_node_hash(pair[0], *pair.get(1).unwrap_or(&pair[0])));
            }
            position /= 2;
            hashes = next;
        }
        if hashes[0] != self.record.boundary_root() {
            return Err(ReplayWitnessError::Binding);
        }
        Ok(path)
    }
}

pub fn replay_leaf_hash(index: u32, leaf: &[u8]) -> Result<[u8; 32], ReplayWitnessError> {
    if leaf.is_empty() || leaf.len() > MAX_PROGRAM_REPLAY_BYTES as usize {
        return Err(ReplayWitnessError::Bounds);
    }
    let mut hash = Sha256::new();
    hash.update(b"LXP/program-replay-leaf/v1\0");
    hash.update(index.to_be_bytes());
    hash.update(
        u32::try_from(leaf.len())
            .map_err(|_| ReplayWitnessError::Bounds)?
            .to_be_bytes(),
    );
    hash.update(leaf);
    Ok(hash.finalize().into())
}

pub fn replay_node_hash(left: [u8; 32], right: [u8; 32]) -> [u8; 32] {
    let mut hash = Sha256::new();
    hash.update(b"LXP/program-replay-node/v1\0");
    hash.update(left);
    hash.update(right);
    hash.finalize().into()
}

pub fn decode_record(
    bytes: &[u8],
    maximum: usize,
) -> Result<(ProgramReplayRecord, Vec<Vec<u8>>), ReplayWitnessError> {
    bounded(bytes, maximum)?;
    let mut cursor = ReplayCursor::new(bytes);
    if cursor.take(PROGRAM_REPLAY_RECORD_DOMAIN.len())? != PROGRAM_REPLAY_RECORD_DOMAIN
        || cursor.u16()? != 1
    {
        return Err(ReplayWitnessError::Encoding);
    }
    let code_hash = cursor.array()?;
    let input_digest = cursor.array()?;
    let runtime_version = cursor.u16()?;
    let abi_version = cursor.u16()?;
    let fee_schedule_version = cursor.u32()?;
    let metering_schedule_version = cursor.u32()?;
    let profile = ProgramReplayProfile::new(cursor.u32()?, cursor.u32()?)?;
    if bytes.len() > profile.maximum_bytes() as usize {
        return Err(ReplayWitnessError::Bounds);
    }
    let terminal_status = cursor.u8()?;
    let boundary_count = cursor.u32()?;
    let boundary_root: [u8; 32] = cursor.array()?;
    let witness_digest: [u8; 32] = cursor.array()?;
    let witness = cursor.field()?;
    if !cursor.done() || <[u8; 32]>::from(Sha256::digest(witness)) != witness_digest {
        return Err(ReplayWitnessError::Binding);
    }
    let mut cursor = ReplayCursor::new(witness);
    if cursor.take(PROGRAM_REPLAY_WITNESS_DOMAIN.len())? != PROGRAM_REPLAY_WITNESS_DOMAIN
        || cursor.u32()? != boundary_count
        || boundary_count == 0
        || boundary_count > profile.maximum_boundaries()
    {
        return Err(ReplayWitnessError::Encoding);
    }
    let count = usize::try_from(boundary_count).map_err(|_| ReplayWitnessError::Bounds)?;
    if count > witness.len() / 5 {
        return Err(ReplayWitnessError::Bounds);
    }
    let mut leaves = reserve(count)?;
    for _ in 0..count {
        let leaf = cursor.owned_field(maximum)?;
        if leaf.is_empty() {
            return Err(ReplayWitnessError::Encoding);
        }
        leaves.push(leaf);
    }
    if !cursor.done() {
        return Err(ReplayWitnessError::Encoding);
    }
    let mut record_leaves = reserve(leaves.len())?;
    for leaf in &leaves {
        record_leaves.push(copy(leaf)?);
    }
    let record = ProgramReplayRecord::from_captured(
        profile,
        code_hash,
        input_digest,
        runtime_version,
        abi_version,
        fee_schedule_version,
        metering_schedule_version,
        terminal_status,
        record_leaves,
    )?;
    if record.boundary_root() != boundary_root || record.canonical_bytes() != bytes {
        return Err(ReplayWitnessError::Binding);
    }
    Ok((record, leaves))
}
