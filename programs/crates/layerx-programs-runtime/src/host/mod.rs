//! Concrete `wasmi` bindings for the version-one capability ABI.

mod balance;
mod calls;
mod context;
mod crypto;
mod events;
pub(crate) mod memory;
mod oracle;
mod scan;
mod signature;
mod storage;
mod transfer;
mod web;

use wasmi::{Caller, Engine, InstancePre, Linker, Module, Store};

use crate::abi::context::{ContextField, ContextRefusal, ExecutionContext};
use crate::abi::response::{CallResponse, ResponseRefusal, ResponseRegion};
use crate::abi::{Abi, AbiError, ReceiptView, ABI_MODULE};
use crate::calls::{CallGraph, Composition, CompositionRefusal};
use crate::crypto::bigint;
use crate::execute::ExecutionFault;
use crate::fault::{ProgramFailure, RefusalClass, RefusalReason};
use crate::meter::inject::{PRIVATE_CHARGE_FUNCTION, PRIVATE_CHECK_FUNCTION, PRIVATE_METER_MODULE};
use crate::meter::Meter;
use crate::AbiRevision;

use self::memory::{nonnegative, read_fixed, write_guest};

pub(super) const STATUS_DENIED: i32 = -1;
pub(super) const STATUS_INVALID: i32 = -2;
pub(super) const STATUS_BOUNDS: i32 = -3;
pub(super) const STATUS_METER: i32 = -4;
pub(super) const STATUS_EVIDENCE: i32 = -5;
pub(super) const STATUS_ABSENT: i32 = -7;
pub(super) const STATUS_ORACLE_UNKNOWN_MARKET: i32 = -8;
pub(super) const STATUS_ORACLE_MARKET_HALTED: i32 = -9;
pub(super) const COMPOSITION_REFUSED: &str = "program composition refused the call graph";

fn wasmi_usage(meter: crate::MeteredUsage) -> wasmi::ExecutionMeteredUsage {
    wasmi::ExecutionMeteredUsage {
        cpu_fuel: meter.cpu_fuel,
        memory_bytes: meter.memory_bytes,
        storage_read_bytes: meter.storage_read_bytes,
        storage_write_bytes: meter.storage_write_bytes,
        output_values: meter.output_values,
        output_bytes: meter.output_bytes,
        occupancy_byte_batches: meter.occupancy_byte_batches,
        occupancy_fee_units: meter.occupancy_fee_units,
        fee_units: meter.fee_units,
    }
}

/// Per-execution host state owned by a `Store`; the shared linker holds none of it.
#[derive(Debug)]
pub(crate) struct RuntimeState {
    meter: Meter,
    abi: Option<Abi>,
    composition: Option<Composition>,
    refusal: Option<CompositionRefusal>,
    outcome: Option<V2OutcomeRegion>,
    failure_subtree_fuel: Option<u64>,
    failure_graph: Option<CallGraph>,
    protocol_context: Option<ExecutionContext>,
    metering_schedule: crate::FuelSchedule,
    legacy_reference_fuel: bool,
    legacy_reference_engine_committed: u64,
    trace_storage_baseline: crate::storage::Storage,
    v2_host_identity: Option<crate::abi::HostStateIdentity>,
}

#[derive(Debug)]
enum V2OutcomeRegion {
    Response(ResponseRegion),
    Failure(ProgramFailure),
}

/// The versioned host surface sealed after its one construction for an engine.
#[derive(Debug)]
pub(crate) struct HostLinker {
    linker: Linker<RuntimeState>,
    construction_count: usize,
    registered_function_count: usize,
}

impl HostLinker {
    pub(crate) fn instantiate(
        &self,
        store: &mut Store<RuntimeState>,
        module: &Module,
    ) -> Result<InstancePre, wasmi::Error> {
        self.linker.instantiate(store, module)
    }

    pub(crate) const fn construction_count(&self) -> usize {
        self.construction_count
    }

    pub(crate) const fn registered_function_count(&self) -> usize {
        self.registered_function_count
    }
}

fn write_execution_fault(
    fault: &ExecutionFault,
    write: &mut dyn FnMut(&[u8]) -> Result<(), AbiError>,
) -> Result<(), AbiError> {
    match fault {
        ExecutionFault::UnknownExport { name } => {
            write(&[0])?;
            write(
                &u32::try_from(name.len())
                    .map_err(|_| AbiError::InvalidEncoding)?
                    .to_be_bytes(),
            )?;
            write(name.as_bytes())?;
        }
        ExecutionFault::NotAFunction { name } => {
            write(&[1])?;
            write(
                &u32::try_from(name.len())
                    .map_err(|_| AbiError::InvalidEncoding)?
                    .to_be_bytes(),
            )?;
            write(name.as_bytes())?;
        }
        ExecutionFault::UnreachableExecuted => write(&[2])?,
        ExecutionFault::MemoryOutOfBounds => write(&[3])?,
        ExecutionFault::TableOutOfBounds => write(&[4])?,
        ExecutionFault::IndirectCallToNull => write(&[5])?,
        ExecutionFault::IntegerDivisionByZero => write(&[6])?,
        ExecutionFault::IntegerOverflow => write(&[7])?,
        ExecutionFault::BadConversionToInteger => write(&[8])?,
        ExecutionFault::StackExhausted => write(&[9])?,
        ExecutionFault::BadSignature => write(&[10])?,
        ExecutionFault::OutOfFuel => write(&[11])?,
        ExecutionFault::GrowthLimited => write(&[12])?,
        ExecutionFault::Resource { refusal } => {
            write(&[13])?;
            write(&crate::abi::abi_error_bytes(&AbiError::Meter(*refusal)))?;
        }
        ExecutionFault::NonIntegerValue => write(&[14])?,
        ExecutionFault::EngineFault { .. } => return Err(AbiError::InvalidEncoding),
    }
    Ok(())
}

fn write_response_refusal(
    refusal: &crate::abi::ResponseRefusal,
    write: &mut dyn FnMut(&[u8]) -> Result<(), AbiError>,
) -> Result<(), AbiError> {
    let mut failed = false;
    refusal.canonical_write(|bytes| {
        if write(bytes).is_err() {
            failed = true;
        }
    });
    if failed {
        return Err(AbiError::InvalidEncoding);
    }
    Ok(())
}

fn abi_revision_byte(revision: AbiRevision) -> u8 {
    match revision {
        AbiRevision::V1 => 1,
        AbiRevision::V2 => 2,
        AbiRevision::V3 => 3,
        AbiRevision::V4 => 4,
    }
}

fn write_composition_refusal(
    refusal: &CompositionRefusal,
    write: &mut dyn FnMut(&[u8]) -> Result<(), AbiError>,
) -> Result<(), AbiError> {
    match refusal {
        CompositionRefusal::NotComposable => write(&[0])?,
        CompositionRefusal::ActivityEvidenceRequired => write(&[1])?,
        CompositionRefusal::ActivityEvidenceMismatch => write(&[2])?,
        CompositionRefusal::ActivityEvidenceReused => write(&[3])?,
        CompositionRefusal::WrongVersion { expected, actual } => {
            write(&[4, abi_revision_byte(*expected), abi_revision_byte(*actual)])?;
        }
        CompositionRefusal::MeteringPlanMismatch { expected, actual } => {
            write(&[5])?;
            write(expected)?;
            write(actual)?;
        }
        CompositionRefusal::UnknownProgram { program } => {
            write(&[6])?;
            write(&program.bytes())?;
        }
        CompositionRefusal::Reentrancy { program } => {
            write(&[7])?;
            write(&program.bytes())?;
        }
        CompositionRefusal::DepthExceeded { limit, attempted } => {
            write(&[8])?;
            write(&limit.to_be_bytes())?;
            write(&attempted.to_be_bytes())?;
        }
        CompositionRefusal::EdgesExceeded { limit, attempted } => {
            write(&[9])?;
            write(&limit.to_be_bytes())?;
            write(&attempted.to_be_bytes())?;
        }
        CompositionRefusal::FanoutExceeded { limit, attempted } => {
            write(&[10])?;
            write(&limit.to_be_bytes())?;
            write(&attempted.to_be_bytes())?;
        }
        CompositionRefusal::VisitsExceeded {
            program,
            limit,
            attempted,
        } => {
            write(&[11])?;
            write(&program.bytes())?;
            write(&limit.to_be_bytes())?;
            write(&attempted.to_be_bytes())?;
        }
        CompositionRefusal::MissingEntry => write(&[12])?,
        CompositionRefusal::MissingAllocator => write(&[13])?,
        CompositionRefusal::MissingMemory => write(&[14])?,
        CompositionRefusal::AllocationRefused { code } => {
            write(&[15])?;
            write(&code.to_be_bytes())?;
        }
        CompositionRefusal::InputTooLarge { bytes, limit } => {
            write(&[16])?;
            write(
                &u64::try_from(*bytes)
                    .map_err(|_| AbiError::InvalidEncoding)?
                    .to_be_bytes(),
            )?;
            write(
                &u64::try_from(*limit)
                    .map_err(|_| AbiError::InvalidEncoding)?
                    .to_be_bytes(),
            )?;
        }
        CompositionRefusal::GuestRefused { program, code } => {
            write(&[17])?;
            write(&program.bytes())?;
            write(&code.to_be_bytes())?;
        }
        CompositionRefusal::Program(failure) => {
            write(&[18])?;
            let failure = failure.canonical_encode();
            write(
                &u32::try_from(failure.len())
                    .map_err(|_| AbiError::InvalidEncoding)?
                    .to_be_bytes(),
            )?;
            write(&failure)?;
        }
        CompositionRefusal::Authority(error) => {
            write(&[19])?;
            write(&crate::abi::abi_error_bytes(error))?;
        }
        CompositionRefusal::Fault(fault) => {
            write(&[20])?;
            write_execution_fault(fault, write)?;
        }
        CompositionRefusal::Resource(refusal) => {
            write(&[21])?;
            write(&crate::abi::abi_error_bytes(&AbiError::Meter(*refusal)))?;
        }
        CompositionRefusal::Response(refusal) => {
            write(&[22])?;
            write_response_refusal(refusal, write)?;
        }
    }
    Ok(())
}

fn supplement_snapshot_bytes(
    charge: &wasmi::ObservationCharge,
) -> Result<u64, wasmi::ExecutionObserverError> {
    let engine_bytes = charge
        .total_bytes()
        .and_then(|bytes| bytes.checked_sub(charge.retained_instruction_bytes))
        .and_then(|bytes| bytes.checked_sub(charge.host_state_bytes))
        .and_then(|bytes| bytes.checked_sub(charge.instance_state_bytes))
        .ok_or(wasmi::ExecutionObserverError::SupplementRejected)?;
    let snapshot_bytes = 214_u64
        .checked_add(engine_bytes)
        .ok_or(wasmi::ExecutionObserverError::SupplementRejected)?;
    Ok(snapshot_bytes)
}

type SupplementOverlay = Vec<(Vec<u8>, Option<Vec<u8>>)>;

struct SupplementHostState {
    bytes: u64,
    identity: Option<crate::abi::HostStateIdentity>,
    isolated: Option<crate::abi::HostStateCommitment>,
}

impl RuntimeState {
    fn v2_identity(
        abi: &Abi,
        baseline: &crate::storage::Storage,
    ) -> Option<crate::abi::HostStateIdentity> {
        abi.v2_host_state_identity(baseline).ok()
    }

    fn trace_storage_entries(abi: &Abi) -> crate::storage::Storage {
        abi.storage_snapshot()
    }

    fn write_v2_runtime_state(
        &self,
        abi: &crate::abi::HostStateCommitment,
        write: &mut dyn FnMut(&[u8]) -> Result<(), AbiError>,
    ) -> Result<(), AbiError> {
        write(b"LayerX/programs/v2/runtime-host-state\0")?;
        write(&abi.root)?;
        write(&abi.canonical_bytes.to_be_bytes())?;
        let usage = self.meter.execution_trace_usage()?;
        write(&usage.cpu_fuel.to_be_bytes())?;
        write(&usage.memory_bytes.to_be_bytes())?;
        write(&usage.storage_read_bytes.to_be_bytes())?;
        write(&usage.storage_write_bytes.to_be_bytes())?;
        write(&usage.output_values.to_be_bytes())?;
        write(&usage.output_bytes.to_be_bytes())?;
        write(&usage.occupancy_byte_batches.to_be_bytes())?;
        write(&usage.occupancy_fee_units.to_be_bytes())?;
        write(&usage.fee_units.to_be_bytes())?;
        write(&self.meter.cpu_remaining().to_be_bytes())?;
        match self.failure_subtree_fuel {
            None => write(&[0])?,
            Some(value) => {
                write(&[1])?;
                write(&value.to_be_bytes())?;
            }
        }
        write(&self.legacy_reference_engine_committed.to_be_bytes())?;
        write(&self.metering_schedule.canonical_bytes())?;
        write(&[u8::from(self.legacy_reference_fuel)])?;
        match self.protocol_context {
            None => write(&[0])?,
            Some(context) => {
                write(&[1])?;
                write(&context.canonical_bytes())?;
            }
        }
        for graph in [
            self.composition.as_ref().map(Composition::graph),
            self.failure_graph.as_ref(),
        ] {
            match graph {
                None => write(&[0])?,
                Some(graph) => {
                    let graph = graph.canonical_evidence();
                    write(&[1])?;
                    write(
                        &u64::try_from(graph.len())
                            .map_err(|_| AbiError::InvalidEncoding)?
                            .to_be_bytes(),
                    )?;
                    write(&graph)?;
                }
            }
        }
        match &self.refusal {
            None => write(&[0])?,
            Some(refusal) => {
                write(&[1])?;
                write_composition_refusal(refusal, write)?;
            }
        }
        match &self.outcome {
            None => write(&[0])?,
            Some(V2OutcomeRegion::Response(response)) => {
                write(&[1])?;
                write(
                    &response
                        .canonical_state_len()
                        .map_err(|()| AbiError::InvalidEncoding)?
                        .to_be_bytes(),
                )?;
                let mut failed = false;
                response.canonical_state_write(|bytes| {
                    if write(bytes).is_err() {
                        failed = true;
                    }
                });
                if failed {
                    return Err(AbiError::InvalidEncoding);
                }
            }
            Some(V2OutcomeRegion::Failure(failure)) => {
                let failure = failure.canonical_encode();
                write(&[2])?;
                write(
                    &u64::try_from(failure.len())
                        .map_err(|_| AbiError::InvalidEncoding)?
                        .to_be_bytes(),
                )?;
                write(&failure)?;
            }
        }
        Ok(())
    }

    fn v2_host_state(&self, hash: bool) -> Result<crate::abi::HostStateCommitment, AbiError> {
        use sha2::{Digest, Sha256};
        let abi_state = self.abi.as_ref().ok_or(AbiError::WrongVersion)?;
        let abi = if hash {
            abi_state.v2_host_state_commitment()?
        } else {
            abi_state.v2_host_state_measurement()?
        };
        let write_state = |write: &mut dyn FnMut(&[u8]) -> Result<(), AbiError>| {
            self.write_v2_runtime_state(&abi, write)
        };
        let mut runtime_bytes = 0_u64;
        write_state(&mut |bytes| {
            runtime_bytes = runtime_bytes
                .checked_add(u64::try_from(bytes.len()).map_err(|_| AbiError::InvalidEncoding)?)
                .ok_or(AbiError::InvalidEncoding)?;
            if runtime_bytes > crate::MAX_ARBITRATION_HOST_STATE_BYTES as u64 {
                return Err(AbiError::InvalidEncoding);
            }
            Ok(())
        })?;
        if !hash {
            return Ok(crate::abi::HostStateCommitment {
                root: [0; 32],
                canonical_bytes: abi
                    .canonical_bytes
                    .checked_add(runtime_bytes)
                    .ok_or(AbiError::InvalidEncoding)?,
            });
        }
        let mut written = 0_u64;
        let mut hasher = Sha256::new();
        write_state(&mut |bytes| {
            written = written
                .checked_add(u64::try_from(bytes.len()).map_err(|_| AbiError::InvalidEncoding)?)
                .ok_or(AbiError::InvalidEncoding)?;
            if written > runtime_bytes {
                return Err(AbiError::InvalidEncoding);
            }
            hasher.update(bytes);
            Ok(())
        })?;
        if written != runtime_bytes {
            return Err(AbiError::InvalidEncoding);
        }
        Ok(crate::abi::HostStateCommitment {
            root: hasher.finalize().into(),
            canonical_bytes: abi
                .canonical_bytes
                .checked_add(runtime_bytes)
                .ok_or(AbiError::InvalidEncoding)?,
        })
    }

    pub(crate) fn v2_host_state_commitment(
        &self,
    ) -> Result<crate::abi::HostStateCommitment, AbiError> {
        self.v2_host_state(true)
    }

    pub(crate) fn v2_host_state_measurement(
        &self,
    ) -> Result<crate::abi::HostStateCommitment, AbiError> {
        self.v2_host_state(false)
    }

    pub(crate) fn v2_host_state_identity(&self) -> Result<crate::abi::HostStateIdentity, AbiError> {
        let _ = self.abi.as_ref().ok_or(AbiError::WrongVersion)?.version();
        self.v2_host_identity.ok_or(AbiError::WrongVersion)
    }

    fn uncollected_supplement(
        &self,
    ) -> Result<wasmi::ExecutionSupplement, wasmi::ExecutionObserverError> {
        let meter = self
            .meter
            .execution_trace_usage()
            .map_err(|_| wasmi::ExecutionObserverError::SupplementRejected)?;
        Ok(wasmi::ExecutionSupplement {
            storage_overlay: Vec::new(),
            authoritative_fuel: self.meter.cpu_remaining(),
            authoritative_usage: wasmi_usage(meter),
            canonical_state_bytes: 0,
            commitment_fuel: 0,
            arbitration_host_state_root: [0; 32],
            arbitration_host_state_bytes: 0,
            arbitration_base_state_root: [0; 32],
            arbitration_receipt_oracle_root: [0; 32],
            arbitration_balance_oracle_root: [0; 32],
            arbitration_engine_canonical_bytes: 0,
            arbitration_instance_retained_bytes: 0,
            arbitration_canonical_state_bytes: 0,
            arbitration_commitment_fuel: 0,
        })
    }

    fn measure_supplement_host(
        &self,
        charge: &mut wasmi::ObservationCharge,
    ) -> Result<SupplementHostState, wasmi::ExecutionObserverError> {
        let measured = if self.abi.is_some() {
            let state = self
                .v2_host_state_measurement()
                .map_err(|_| wasmi::ExecutionObserverError::SupplementRejected)?;
            let identity = self
                .v2_host_state_identity()
                .map_err(|_| wasmi::ExecutionObserverError::SupplementRejected)?;
            charge.host_state_bytes = state
                .canonical_bytes
                .checked_mul(3)
                .ok_or(wasmi::ExecutionObserverError::SupplementRejected)?;
            SupplementHostState {
                bytes: state.canonical_bytes,
                identity: Some(identity),
                isolated: None,
            }
        } else {
            use sha2::{Digest, Sha256};
            let state = crate::abi::HostStateCommitment {
                root: Sha256::digest(b"LayerX/programs/v2/isolated-host-state\0").into(),
                canonical_bytes: b"LayerX/programs/v2/isolated-host-state\0".len() as u64,
            };
            let identity = self
                .v2_host_identity
                .ok_or(wasmi::ExecutionObserverError::SupplementRejected)?;
            charge.host_state_bytes = state
                .canonical_bytes
                .checked_mul(3)
                .ok_or(wasmi::ExecutionObserverError::SupplementRejected)?;
            SupplementHostState {
                bytes: state.canonical_bytes,
                identity: Some(identity),
                isolated: Some(state),
            }
        };
        Ok(measured)
    }

    fn supplement_storage_overlay(&self, overlay_entries: usize) -> SupplementOverlay {
        let mut storage_overlay = Vec::with_capacity(overlay_entries);
        if let Some(abi) = self.abi.as_ref() {
            abi.for_each_storage_commitment_delta(&self.trace_storage_baseline, |key, value| {
                storage_overlay.push((key, value.map(<[u8]>::to_vec)));
            });
        } else {
            crate::storage::Storage::new().for_each_commitment_delta(
                &self.trace_storage_baseline,
                |key, value| {
                    storage_overlay.push((key, value.map(<[u8]>::to_vec)));
                },
            );
        }
        storage_overlay.sort_by(|left, right| left.0.cmp(&right.0));
        storage_overlay
    }

    pub(crate) fn execution_supplement(
        &mut self,
        charge: &mut wasmi::ObservationCharge,
        remaining_bytes: u64,
        remaining_work: u64,
    ) -> Result<wasmi::ExecutionSupplement, wasmi::ExecutionObserverError> {
        if !charge.collect {
            return self.uncollected_supplement();
        }
        let (overlay_entries, overlay_bytes) = if let Some(abi) = self.abi.as_ref() {
            abi.storage_commitment_delta_metrics(&self.trace_storage_baseline)
                .ok_or(wasmi::ExecutionObserverError::SupplementRejected)?
        } else {
            crate::storage::Storage::new()
                .commitment_delta_metrics(&self.trace_storage_baseline)
                .ok_or(wasmi::ExecutionObserverError::SupplementRejected)?
        };
        charge.storage_overlay_bytes = overlay_bytes;
        let SupplementHostState {
            bytes: host_state_bytes,
            identity: host_identity,
            isolated: isolated_host_state,
        } = self.measure_supplement_host(charge)?;
        let retained_instruction_bytes = charge.retained_instruction_bytes;
        let arbitration_instance_retained_bytes = charge.instance_state_bytes;
        let snapshot_bytes = supplement_snapshot_bytes(charge)?;
        let retained_bytes = snapshot_bytes
            .checked_add(retained_instruction_bytes)
            .and_then(|bytes| bytes.checked_add(arbitration_instance_retained_bytes))
            .and_then(|bytes| bytes.checked_add(charge.host_state_bytes))
            .and_then(|bytes| bytes.checked_add(charge.arbitration_engine_canonical_bytes))
            .ok_or(wasmi::ExecutionObserverError::SupplementRejected)?;
        if retained_bytes > remaining_bytes || retained_bytes > remaining_work {
            return Err(wasmi::ExecutionObserverError::SnapshotLimitExceeded);
        }
        let snapshot_fuel = crate::step_commitment_fuel(snapshot_bytes)
            .map_err(|_| wasmi::ExecutionObserverError::SupplementRejected)?;
        let arbitration_engine_bytes = charge.arbitration_engine_canonical_bytes;
        let arbitration_fuel = crate::arbitration_step_commitment_fuel_with_host(
            snapshot_bytes,
            arbitration_engine_bytes,
            host_state_bytes,
        )
        .map_err(|_| wasmi::ExecutionObserverError::SupplementRejected)?;
        let total_fuel = snapshot_fuel
            .checked_add(arbitration_fuel)
            .ok_or(wasmi::ExecutionObserverError::SupplementRejected)?;
        self.meter
            .charge_cpu(total_fuel)
            .map_err(|_| wasmi::ExecutionObserverError::SupplementRejected)?;
        let host_state = if let Some(state) = isolated_host_state {
            state
        } else {
            let state = self
                .v2_host_state_commitment()
                .map_err(|_| wasmi::ExecutionObserverError::SupplementRejected)?;
            if state.canonical_bytes != host_state_bytes {
                return Err(wasmi::ExecutionObserverError::SupplementRejected);
            }
            state
        };
        charge.value_bytes = snapshot_bytes
            .checked_add(retained_instruction_bytes)
            .ok_or(wasmi::ExecutionObserverError::SupplementRejected)?;
        charge.frame_bytes = 0;
        charge.local_bytes = 0;
        charge.global_bytes = 0;
        charge.memory_bytes = 0;
        charge.storage_overlay_bytes = 0;
        charge.instruction_bytes = 0;
        charge.retained_instruction_bytes = 0;
        let meter = self
            .meter
            .execution_trace_usage()
            .map_err(|_| wasmi::ExecutionObserverError::SupplementRejected)?;
        let storage_overlay = self.supplement_storage_overlay(overlay_entries);
        Ok(wasmi::ExecutionSupplement {
            storage_overlay,
            authoritative_fuel: self.meter.cpu_remaining(),
            authoritative_usage: wasmi_usage(meter),
            canonical_state_bytes: snapshot_bytes,
            commitment_fuel: snapshot_fuel,
            arbitration_host_state_root: host_state.root,
            arbitration_host_state_bytes: host_state.canonical_bytes,
            arbitration_base_state_root: host_identity
                .map_or([0; 32], |identity| identity.base_state),
            arbitration_receipt_oracle_root: host_identity
                .map_or([0; 32], |identity| identity.receipt_oracle),
            arbitration_balance_oracle_root: host_identity
                .map_or([0; 32], |identity| identity.balance_oracle),
            arbitration_engine_canonical_bytes: arbitration_engine_bytes,
            arbitration_instance_retained_bytes,
            arbitration_canonical_state_bytes: crate::arbitration_step_state_bytes(
                snapshot_bytes,
                arbitration_engine_bytes,
            )
            .map_err(|_| wasmi::ExecutionObserverError::SupplementRejected)?,
            arbitration_commitment_fuel: arbitration_fuel,
        })
    }

    pub(crate) fn isolated(meter: Meter) -> Self {
        use sha2::{Digest, Sha256};
        Self {
            meter,
            abi: None,
            composition: None,
            refusal: None,
            outcome: None,
            failure_subtree_fuel: None,
            failure_graph: None,
            protocol_context: None,
            metering_schedule: crate::FuelSchedule::WASMI_0_31_2,
            legacy_reference_fuel: false,
            legacy_reference_engine_committed: 0,
            trace_storage_baseline: crate::storage::Storage::new(),
            v2_host_identity: Some(crate::abi::HostStateIdentity {
                base_state: Sha256::digest(b"LayerX/programs/v2/isolated-base-state\0").into(),
                receipt_oracle: Sha256::digest(b"LayerX/programs/v2/isolated-receipt-oracle\0")
                    .into(),
                balance_oracle: Sha256::digest(b"LayerX/programs/v2/isolated-balance-oracle\0")
                    .into(),
            }),
        }
    }

    pub(crate) fn composed(meter: Meter, abi: Abi, composition: Composition) -> Self {
        let trace_storage_baseline = Self::trace_storage_entries(&abi);
        let v2_host_identity = Self::v2_identity(&abi, &trace_storage_baseline);
        Self {
            meter,
            abi: Some(abi),
            composition: Some(composition),
            refusal: None,
            outcome: None,
            failure_subtree_fuel: None,
            failure_graph: None,
            protocol_context: None,
            metering_schedule: crate::FuelSchedule::WASMI_0_31_2,
            legacy_reference_fuel: false,
            legacy_reference_engine_committed: 0,
            trace_storage_baseline,
            v2_host_identity,
        }
    }

    pub(crate) fn sandbox(meter: Meter, abi: Abi) -> Self {
        let mut state = Self::isolated(meter);
        state.trace_storage_baseline = Self::trace_storage_entries(&abi);
        state.v2_host_identity = Self::v2_identity(&abi, &state.trace_storage_baseline);
        state.abi = Some(abi);
        state
    }

    pub(crate) fn composed_with_response(
        meter: Meter,
        abi: Abi,
        composition: Composition,
        capacity: usize,
    ) -> Result<Self, ResponseRefusal> {
        let trace_storage_baseline = Self::trace_storage_entries(&abi);
        let v2_host_identity = Self::v2_identity(&abi, &trace_storage_baseline);
        Ok(Self {
            meter,
            abi: Some(abi),
            composition: Some(composition),
            refusal: None,
            outcome: Some(V2OutcomeRegion::Response(ResponseRegion::new(capacity)?)),
            failure_subtree_fuel: None,
            failure_graph: None,
            protocol_context: None,
            metering_schedule: crate::FuelSchedule::WASMI_0_31_2,
            legacy_reference_fuel: false,
            legacy_reference_engine_committed: 0,
            trace_storage_baseline,
            v2_host_identity,
        })
    }

    pub(crate) fn isolated_legacy_reference(meter: Meter) -> Self {
        let mut state = Self::isolated(meter);
        state.legacy_reference_fuel = true;
        state
    }

    pub(crate) const fn uses_legacy_reference_fuel(&self) -> bool {
        self.legacy_reference_fuel
    }

    pub(crate) const fn legacy_reference_engine_committed(&self) -> u64 {
        self.legacy_reference_engine_committed
    }

    pub(crate) fn set_legacy_reference_engine_committed(&mut self, consumed: u64) {
        self.legacy_reference_engine_committed = consumed;
    }

    pub(crate) fn publish_response(
        &mut self,
        response: CallResponse,
    ) -> Result<(), ResponseRefusal> {
        let bytes = response.bytes.len();
        let region = match self.outcome.as_mut() {
            Some(V2OutcomeRegion::Response(region)) => region,
            Some(V2OutcomeRegion::Failure(_)) => return Err(ResponseRefusal::DuplicatePublication),
            None => return Err(ResponseRefusal::CapacityExceeded { bytes, capacity: 0 }),
        };
        region.publish(response)?;
        if let Err(refusal) = self.meter.charge_output_bytes(bytes) {
            let refusal = ResponseRefusal::Meter(refusal);
            region.refuse(refusal.clone());
            return Err(refusal);
        }
        Ok(())
    }

    pub(crate) fn publish_failure(
        &mut self,
        class: RefusalClass,
        reason: RefusalReason,
    ) -> Result<(), ResponseRefusal> {
        match self.outcome.as_ref() {
            Some(V2OutcomeRegion::Failure(_)) => return Err(ResponseRefusal::DuplicatePublication),
            Some(V2OutcomeRegion::Response(region)) if region.has_publication() => {
                return Err(ResponseRefusal::DuplicatePublication)
            }
            Some(V2OutcomeRegion::Response(_)) => {}
            None => return Err(ResponseRefusal::InvalidPublication),
        }
        let program = self
            .abi
            .as_ref()
            .map(Abi::program)
            .ok_or(ResponseRefusal::InvalidPublication)?;
        self.meter
            .charge_output_bytes(reason.bytes().len())
            .map_err(ResponseRefusal::Meter)?;
        self.outcome = Some(V2OutcomeRegion::Failure(ProgramFailure::authenticated(
            program, class, reason,
        )));
        Ok(())
    }

    pub(super) fn publish_failure_status(
        &mut self,
        class: RefusalClass,
        reason: RefusalReason,
    ) -> i32 {
        match self.publish_failure(class, reason) {
            Ok(()) => 0,
            Err(ResponseRefusal::Meter(refusal)) => {
                self.record_refusal(CompositionRefusal::Resource(refusal));
                STATUS_METER
            }
            Err(_) if self.failure().is_some() => STATUS_BOUNDS,
            Err(refusal) => {
                self.record_refusal(CompositionRefusal::Response(refusal));
                STATUS_BOUNDS
            }
        }
    }

    pub(crate) fn failure(&self) -> Option<&ProgramFailure> {
        match self.outcome.as_ref() {
            Some(V2OutcomeRegion::Failure(failure)) => Some(failure),
            _ => None,
        }
    }

    pub(crate) fn set_failure_subtree_fuel(&mut self, fuel: u64) {
        self.failure_subtree_fuel = Some(fuel);
    }

    pub(crate) fn take_failure_subtree_fuel(&mut self) -> Option<u64> {
        self.failure_subtree_fuel.take()
    }

    pub(crate) fn set_failure_graph(&mut self, graph: CallGraph) {
        self.failure_graph = Some(graph);
    }

    pub(crate) fn take_failure_graph(&mut self) -> Option<CallGraph> {
        self.failure_graph.take()
    }

    pub(crate) fn failure_graph(&self) -> Option<&CallGraph> {
        self.failure_graph.as_ref()
    }

    pub(crate) fn authenticate_protocol_context(&mut self, context: ExecutionContext) {
        self.protocol_context = Some(context);
    }

    pub(crate) fn context_field(&self, field: ContextField) -> Result<Vec<u8>, ContextRefusal> {
        let context = self
            .protocol_context
            .ok_or(ContextRefusal::Unauthenticated)?;
        let abi = self.abi.as_ref().ok_or(ContextRefusal::Unauthenticated)?;
        let graph = self
            .composition
            .as_ref()
            .ok_or(ContextRefusal::Unauthenticated)?
            .graph();
        let current = graph.current().ok_or(ContextRefusal::FrameMismatch)?;
        if current.program() != abi.program()
            || current.principal() != abi.principal()
            || current.id() != abi.frame()
        {
            return Err(ContextRefusal::FrameMismatch);
        }
        let immediate_caller = graph.immediate_caller();
        let remaining_fuel = self.meter.cpu_remaining();
        Ok(context.encode(
            field,
            current.program(),
            immediate_caller,
            current.principal(),
            remaining_fuel,
        ))
    }

    pub(crate) const fn protocol_context(&self) -> Option<ExecutionContext> {
        self.protocol_context
    }

    pub(super) fn publish_response_status(&mut self, response: CallResponse) -> i32 {
        match self.publish_response(response) {
            Ok(()) => 0,
            Err(ResponseRefusal::Meter(_)) => STATUS_METER,
            Err(_) => STATUS_BOUNDS,
        }
    }

    pub(crate) fn finalize_response(&self, code: i32) -> Result<CallResponse, ResponseRefusal> {
        self.outcome.as_ref().map_or_else(
            || {
                Ok(CallResponse {
                    code,
                    bytes: Vec::new(),
                })
            },
            |outcome| match outcome {
                V2OutcomeRegion::Response(region) => region.finish(code),
                V2OutcomeRegion::Failure(_) => Err(ResponseRefusal::DuplicatePublication),
            },
        )
    }

    pub(crate) fn refuse_response(&mut self, refusal: ResponseRefusal) {
        if let Some(V2OutcomeRegion::Response(region)) = self.outcome.as_mut() {
            region.refuse(refusal);
        }
    }

    pub(crate) const fn meter(&self) -> &Meter {
        &self.meter
    }

    pub(crate) fn meter_mut(&mut self) -> &mut Meter {
        &mut self.meter
    }

    pub(crate) fn frame_cpu_consumed(&self) -> Result<u64, crate::meter::MeterRefusal> {
        self.meter
            .cpu_total()
            .checked_sub(self.meter.cpu_carried())
            .ok_or(crate::meter::MeterRefusal::CounterOverflow {
                resource: crate::meter::ResourceKind::Cpu,
            })
    }

    pub(crate) fn set_meter(&mut self, meter: Meter) {
        self.meter = meter;
    }

    pub(crate) fn bind_metering_schedule(&mut self, schedule: crate::FuelSchedule) {
        self.metering_schedule = schedule;
    }

    pub(crate) const fn metering_schedule_version(&self) -> u32 {
        self.metering_schedule.version()
    }

    pub(crate) const fn metering_schedule(&self) -> crate::FuelSchedule {
        self.metering_schedule
    }

    pub(crate) fn authorization_abi(&self) -> Option<&Abi> {
        self.abi.as_ref()
    }

    pub(crate) fn abi_mut(&mut self) -> Option<&mut Abi> {
        self.abi.as_mut()
    }

    pub(crate) fn composition(&self) -> Option<&Composition> {
        self.composition.as_ref()
    }

    pub(crate) fn composition_mut(&mut self) -> Option<&mut Composition> {
        self.composition.as_mut()
    }

    pub(crate) fn record_refusal(&mut self, refusal: CompositionRefusal) {
        if self.refusal.is_none() && self.failure().is_none() {
            self.refusal = Some(refusal);
        }
    }

    pub(crate) fn refusal(&self) -> Option<&CompositionRefusal> {
        self.refusal.as_ref()
    }

    pub(crate) fn into_parts(self) -> (Meter, Option<Abi>, Option<Composition>) {
        (self.meter, self.abi, self.composition)
    }

    pub(super) fn with_abi<T>(
        &mut self,
        operation: impl FnOnce(&mut Abi, &mut Meter) -> Result<T, AbiError>,
    ) -> Result<T, AbiError> {
        let result = self
            .abi
            .as_mut()
            .ok_or(AbiError::CapabilityDenied)
            .and_then(|abi| operation(abi, &mut self.meter));
        if let Err(error) = &result {
            if self.meter.is_activity() {
                self.record_refusal(CompositionRefusal::Authority(error.clone()));
            }
        }
        result
    }
}

pub(crate) fn charge_host_cpu(
    caller: &mut Caller<'_, RuntimeState>,
    fuel: u64,
) -> Result<(), crate::meter::MeterRefusal> {
    if caller.data().uses_legacy_reference_fuel() {
        reconcile_reference_guest_cpu(caller)?;
        if caller.consume_fuel(fuel).is_err() {
            caller.data_mut().meter_mut().mark_cpu_exhausted();
            return Err(caller.data().meter().exhaustion().unwrap_or(
                crate::meter::MeterRefusal::BudgetExceeded {
                    resource: crate::meter::ResourceKind::Cpu,
                    limit: caller.data().meter().cpu_budget(),
                    attempted: caller.data().meter().cpu_budget().saturating_add(1),
                },
            ));
        }
        caller.data_mut().meter_mut().charge_cpu(fuel)?;
        let consumed = caller.fuel_consumed().unwrap_or_else(|| unreachable!());
        caller
            .data_mut()
            .set_legacy_reference_engine_committed(consumed);
        return Ok(());
    }
    caller.data_mut().meter_mut().charge_cpu(fuel)
}

pub(crate) fn reconcile_reference_guest_cpu(
    caller: &mut Caller<'_, RuntimeState>,
) -> Result<(), crate::meter::MeterRefusal> {
    if !caller.data().uses_legacy_reference_fuel() {
        return Ok(());
    }
    let consumed = caller.fuel_consumed().unwrap_or(0);
    let committed = caller.data().legacy_reference_engine_committed();
    let guest =
        consumed
            .checked_sub(committed)
            .ok_or(crate::meter::MeterRefusal::CounterOverflow {
                resource: crate::meter::ResourceKind::Cpu,
            })?;
    caller.data_mut().meter_mut().charge_cpu(guest)?;
    caller
        .data_mut()
        .set_legacy_reference_engine_committed(consumed);
    Ok(())
}

#[allow(clippy::too_many_lines)]
pub(crate) fn linker(engine: &Engine) -> Result<HostLinker, ExecutionFault> {
    let mut linker = Linker::new(engine);
    linker
        .func_wrap(
            PRIVATE_METER_MODULE,
            PRIVATE_CHECK_FUNCTION,
            |caller: Caller<'_, RuntimeState>, raw_charge: i64| -> Result<(), wasmi::core::Trap> {
                let charge = u64::try_from(raw_charge)
                    .map_err(|_| wasmi::core::Trap::from(wasmi::core::TrapCode::OutOfFuel))?;
                let meter = caller.data().meter();
                match meter.cpu_total().checked_add(charge) {
                    Some(attempted) if attempted <= meter.cpu_budget() => Ok(()),
                    _ => Err(wasmi::core::Trap::from(wasmi::core::TrapCode::OutOfFuel)),
                }
            },
        )
        .map_err(|error| linker_fault(&error))?;
    linker
        .func_wrap(
            PRIVATE_METER_MODULE,
            PRIVATE_CHARGE_FUNCTION,
            |mut caller: Caller<'_, RuntimeState>,
             raw_charge: i64|
             -> Result<(), wasmi::core::Trap> {
                let charge = u64::try_from(raw_charge)
                    .map_err(|_| wasmi::core::Trap::from(wasmi::core::TrapCode::OutOfFuel))?;
                caller
                    .data_mut()
                    .meter_mut()
                    .charge_cpu(charge)
                    .map_err(|_| wasmi::core::Trap::from(wasmi::core::TrapCode::OutOfFuel))
            },
        )
        .map_err(|error| linker_fault(&error))?;
    storage::register(&mut linker)?;
    events::register(&mut linker)?;
    calls::register(&mut linker)?;
    crypto::register(&mut linker)?;
    signature::register(&mut linker)?;
    bigint::register(&mut linker)?;
    calls::register_v2(&mut linker)?;
    context::register_v2(&mut linker)?;
    scan::register_v2(&mut linker)?;
    storage::register_v2(&mut linker)?;
    transfer::register_v2(&mut linker)?;
    balance::register_v2(&mut linker)?;
    oracle::register_v3(&mut linker)?;
    web::register_v4(&mut linker)?;
    transfer::register(&mut linker)?;
    linker
        .func_wrap(
            ABI_MODULE,
            "receipt_read",
            |mut caller: Caller<'_, RuntimeState>,
             digest_pointer: i32,
             digest_length: i32,
             output_pointer: i32,
             output_capacity: i32|
             -> i32 {
                let digest = match read_fixed::<32>(&caller, digest_pointer, digest_length) {
                    Ok(digest) => digest,
                    Err(status) => return status,
                };
                let view = match caller
                    .data_mut()
                    .with_abi(|abi, _| abi.receipt_read(digest))
                {
                    Ok(view) => view,
                    Err(error) => return error_status(&error),
                };
                let encoded = encode_receipt(&view);
                let capacity = match nonnegative(output_capacity) {
                    Ok(capacity) => capacity,
                    Err(status) => return status,
                };
                if encoded.len() > capacity {
                    return STATUS_BOUNDS;
                }
                if let Err(status) = write_guest(&mut caller, output_pointer, &encoded) {
                    return status;
                }
                i32::try_from(encoded.len()).unwrap_or(STATUS_BOUNDS)
            },
        )
        .map_err(|error| linker_fault(&error))?;
    let registered_function_count = crate::abi::HOST_FUNCTIONS.len()
        + crate::abi::manifest::ABI_V2_HOST_FUNCTIONS.len()
        + crate::abi::manifest::ABI_V3_HOST_FUNCTIONS.len()
        + crate::abi::manifest::ABI_V4_HOST_FUNCTIONS.len();
    Ok(HostLinker {
        linker,
        construction_count: 1,
        registered_function_count,
    })
}

fn encode_receipt(view: &ReceiptView) -> Vec<u8> {
    let mut encoded = Vec::with_capacity(116);
    encoded.extend_from_slice(&view.receipt_digest);
    encoded.extend_from_slice(&view.result_code.to_be_bytes());
    encoded.extend_from_slice(&view.asset);
    encoded.extend_from_slice(&view.amount.to_be_bytes());
    encoded.extend_from_slice(&view.state_root);
    encoded
}

pub(crate) const fn error_status(error: &AbiError) -> i32 {
    match error {
        AbiError::CapabilityDenied
        | AbiError::CapabilityEscalation
        | AbiError::AccessDeclaration => STATUS_DENIED,
        AbiError::Meter(_) => STATUS_METER,
        AbiError::ReceiptMismatch | AbiError::BalanceEvidenceUnavailable => STATUS_EVIDENCE,
        AbiError::BalanceAbsent => STATUS_ABSENT,
        AbiError::OracleUnknownMarket => STATUS_ORACLE_UNKNOWN_MARKET,
        AbiError::OracleMarketHalted => STATUS_ORACLE_MARKET_HALTED,
        AbiError::Storage(
            crate::storage::StorageError::InvalidScanCursor
            | crate::storage::StorageError::InvalidScanLimits,
        )
        | AbiError::WrongVersion
        | AbiError::InvalidCapability
        | AbiError::DuplicateCapability
        | AbiError::InvalidEncoding => STATUS_INVALID,
        AbiError::EventBounds
        | AbiError::CallBounds
        | AbiError::AmountBounds
        | AbiError::Storage(_) => STATUS_BOUNDS,
    }
}

pub(crate) fn linker_fault(error: &wasmi::errors::LinkerError) -> ExecutionFault {
    ExecutionFault::EngineFault {
        reason: error.to_string(),
    }
}

impl RuntimeState {
    pub(crate) fn replay_host_witness(&self, code_hash: [u8; 32], maximum: usize) -> Result<crate::replay::ReplayHostWitnessV1, crate::replay::ReplayWitnessError> {
        use crate::replay::{append, ReplayHostWitnessV1, ReplayWitnessError as E, STORAGE_DOMAIN};
        if code_hash == [0; 32] { return Err(E::Binding); }
        let mut remaining = ReplayHostWitnessV1::payload_budget(maximum, self.abi.is_some())?;
        let meter = self.meter.replay_state_bytes()?;
        remaining = remaining.checked_sub(meter.len()).ok_or(E::Bounds)?;
        let abi = self.abi.as_ref().map(|abi| abi.replay_host_preimage(remaining.min(crate::MAX_ARBITRATION_HOST_STATE_BYTES))).transpose()?;
        let abi_bytes = abi.as_ref().map_or(0, Vec::len);
        remaining = remaining.checked_sub(abi_bytes).ok_or(E::Bounds)?;
        let runtime_limit = remaining.min(crate::MAX_ARBITRATION_HOST_STATE_BYTES.checked_sub(abi_bytes).ok_or(E::Bounds)?);
        let mut runtime = Vec::new();
        if let Some(abi_bytes) = &abi {
            use sha2::{Digest, Sha256};
            let abi_commitment = crate::abi::HostStateCommitment { root: Sha256::digest(abi_bytes).into(), canonical_bytes: abi_bytes.len() as u64 };
            let mut failure = None;
            let written = self.write_v2_runtime_state(&abi_commitment, &mut |bytes| {
                append(&mut runtime, bytes, runtime_limit).map_err(|error| { failure = Some(error); AbiError::InvalidEncoding })
            });
            if written.is_err() { return Err(failure.unwrap_or(E::StateUnavailable)); }
        } else {
            append(&mut runtime, b"LayerX/programs/v2/isolated-host-state\0", runtime_limit)?;
        }
        remaining = remaining.checked_sub(runtime.len()).ok_or(E::Bounds)?;
        let mut baseline = Vec::new();
        append(&mut baseline, STORAGE_DOMAIN, remaining)?;
        self.trace_storage_baseline.try_for_each_commitment_entry(|key, value| {
            append(&mut baseline, &u32::try_from(key.len()).map_err(|_| E::Bounds)?.to_be_bytes(), remaining)?;
            append(&mut baseline, &key, remaining)?;
            append(&mut baseline, &u32::try_from(value.len()).map_err(|_| E::Bounds)?.to_be_bytes(), remaining)?;
            append(&mut baseline, value, remaining)
        })?;
        let witness = ReplayHostWitnessV1::from_parts(code_hash, abi, runtime, meter, baseline, maximum)?;
        let mut charge = wasmi::ObservationCharge::default();
        let host = self.measure_supplement_host(&mut charge).map_err(|_| E::StateUnavailable)?;
        let commitment = match host.isolated { Some(commitment) => commitment, None => self.v2_host_state_commitment().map_err(|_| E::StateUnavailable)? };
        let identity = host.identity.ok_or(E::StateUnavailable)?;
        witness.compare_v2_preimages(code_hash, commitment.root, commitment.canonical_bytes, identity.base_state)?;
        Ok(witness)
    }
}

impl RuntimeState {
    pub(crate) fn replay_storage_witness(&self, code_hash: [u8; 32], maximum: usize) -> Result<crate::replay::StorageReplayWitnessV1, crate::replay::ReplayWitnessError> {
        self.abi.as_ref().ok_or(crate::replay::ReplayWitnessError::StateUnavailable)?
            .replay_storage_witness(code_hash, &self.trace_storage_baseline, maximum)
    }
}

fn replay_execution_fault(cursor: &mut crate::replay::ReplayCursor<'_>) -> Result<ExecutionFault, crate::replay::ReplayWitnessError> {
    use crate::replay::ReplayWitnessError as E;
    Ok(match cursor.u8()? {
        tag @ (0 | 1) => {
            let bytes = cursor.owned_field(crate::MAX_ARBITRATION_HOST_STATE_BYTES)?;
            let name = String::from_utf8(bytes).map_err(|_| E::Encoding)?;
            if tag == 0 { ExecutionFault::UnknownExport { name } } else { ExecutionFault::NotAFunction { name } }
        }
        2 => ExecutionFault::UnreachableExecuted, 3 => ExecutionFault::MemoryOutOfBounds,
        4 => ExecutionFault::TableOutOfBounds, 5 => ExecutionFault::IndirectCallToNull,
        6 => ExecutionFault::IntegerDivisionByZero, 7 => ExecutionFault::IntegerOverflow,
        8 => ExecutionFault::BadConversionToInteger, 9 => ExecutionFault::StackExhausted,
        10 => ExecutionFault::BadSignature, 11 => ExecutionFault::OutOfFuel, 12 => ExecutionFault::GrowthLimited,
        13 => ExecutionFault::Resource { refusal: replay_resource_refusal(cursor)? },
        14 => ExecutionFault::NonIntegerValue, _ => return Err(E::Encoding),
    })
}
fn replay_resource_refusal(cursor: &mut crate::replay::ReplayCursor<'_>) -> Result<crate::MeterRefusal, crate::replay::ReplayWitnessError> {
    match crate::abi::decode_replay_abi_error(cursor)? {
        AbiError::Meter(refusal) => Ok(refusal), _ => Err(crate::replay::ReplayWitnessError::Encoding),
    }
}
fn replay_composition_refusal(cursor: &mut crate::replay::ReplayCursor<'_>) -> Result<CompositionRefusal, crate::replay::ReplayWitnessError> {
    use crate::replay::ReplayWitnessError as E;
    fn program(cursor: &mut crate::replay::ReplayCursor<'_>) -> Result<crate::storage::ProgramId, E> {
        crate::storage::ProgramId::new(cursor.array()?).map_err(|_| E::Encoding)
    }
    fn revision(cursor: &mut crate::replay::ReplayCursor<'_>) -> Result<AbiRevision, E> {
        match cursor.u8()? { 1 => Ok(AbiRevision::V1), 2 => Ok(AbiRevision::V2), 3 => Ok(AbiRevision::V3), 4 => Ok(AbiRevision::V4), _ => Err(E::Encoding) }
    }
    Ok(match cursor.u8()? {
        0 => CompositionRefusal::NotComposable, 1 => CompositionRefusal::ActivityEvidenceRequired,
        2 => CompositionRefusal::ActivityEvidenceMismatch, 3 => CompositionRefusal::ActivityEvidenceReused,
        4 => CompositionRefusal::WrongVersion { expected: revision(cursor)?, actual: revision(cursor)? },
        5 => CompositionRefusal::MeteringPlanMismatch { expected: crate::calls::MeteringPlanIdentity::from_untrusted_replay_bytes(cursor.array()?), actual: crate::calls::MeteringPlanIdentity::from_untrusted_replay_bytes(cursor.array()?) },
        6 => CompositionRefusal::UnknownProgram { program: program(cursor)? },
        7 => CompositionRefusal::Reentrancy { program: program(cursor)? },
        8 => CompositionRefusal::DepthExceeded { limit: cursor.u32()?, attempted: cursor.u32()? },
        9 => CompositionRefusal::EdgesExceeded { limit: cursor.u32()?, attempted: cursor.u32()? },
        10 => CompositionRefusal::FanoutExceeded { limit: cursor.u32()?, attempted: cursor.u32()? },
        11 => CompositionRefusal::VisitsExceeded { program: program(cursor)?, limit: cursor.u32()?, attempted: cursor.u32()? },
        12 => CompositionRefusal::MissingEntry, 13 => CompositionRefusal::MissingAllocator, 14 => CompositionRefusal::MissingMemory,
        15 => CompositionRefusal::AllocationRefused { code: cursor.i32()? },
        16 => CompositionRefusal::InputTooLarge { bytes: cursor.usize64()?, limit: cursor.usize64()? },
        17 => CompositionRefusal::GuestRefused { program: program(cursor)?, code: cursor.i32()? },
        18 => CompositionRefusal::Program(ProgramFailure::canonical_decode(cursor.field()?).map_err(|_| E::Encoding)?),
        19 => CompositionRefusal::Authority(crate::abi::decode_replay_abi_error(cursor)?),
        20 => CompositionRefusal::Fault(replay_execution_fault(cursor)?),
        21 => CompositionRefusal::Resource(replay_resource_refusal(cursor)?),
        22 => CompositionRefusal::Response(ResponseRefusal::decode_untrusted_replay(cursor)?),
        _ => return Err(E::Encoding),
    })
}

impl RuntimeState {
    pub(crate) fn capture_semantic_replay_source(&self, code_hash: [u8; 32], maximum: usize) -> Result<crate::replay::CapturedSemanticReplayV2, crate::replay::ReplayWitnessError> {
        use crate::replay::{CapturedSemanticReplayV2, ReplayWitnessError as E};
        let abi = self.abi.as_ref().ok_or(E::StateUnavailable)?;
        let host = self.replay_host_witness(code_hash, maximum)?;
        let composition = self.replay_composition_witness(code_hash, maximum)?;
        let resolver = self.composition.as_ref().map(Composition::resolver);
        let storage_budget = CapturedSemanticReplayV2::storage_budget(maximum, &host, &composition)?;
        let storage = self.replay_storage_witness(code_hash, storage_budget)?;
        let authority = abi.capture_replay_authority(host.abi_preimage()?, maximum)?;
        CapturedSemanticReplayV2::from_capture(host, storage, authority, composition, resolver, maximum)
    }

    pub(crate) fn restore_untrusted_semantic(host: &crate::replay::ReplayHostWitnessV1, pair: crate::replay::UntrustedStorageReplayPair, authority: &crate::abi::CapturedAbiReplayAuthority, graphs: crate::replay::UntrustedCompositionReplayState, resolver: Option<std::rc::Rc<dyn crate::ProgramResolver>>) -> Result<Self, crate::replay::ReplayWitnessError> {
        use crate::replay::{append, ReplayCursor, ReplayWitnessError as E};
        use sha2::{Digest, Sha256};
        let bytes = host.runtime_preimage();
        if bytes.len() > crate::MAX_ARBITRATION_HOST_STATE_BYTES { return Err(E::Bounds); }
        let abi_bytes = host.abi_preimage()?;
        let abi = Abi::restore_untrusted_host_preimage(abi_bytes, pair.current, authority)?;
        let meter = host.decode_untrusted_meter()?;
        let commitment = crate::abi::HostStateCommitment { root: Sha256::digest(abi_bytes).into(), canonical_bytes: abi_bytes.len() as u64 };
        let mut cursor = ReplayCursor::new(bytes);
        if cursor.take(b"LayerX/programs/v2/runtime-host-state\0".len())? != b"LayerX/programs/v2/runtime-host-state\0"
            || cursor.array::<32>()? != commitment.root || cursor.u64()? != commitment.canonical_bytes { return Err(E::Binding); }
        let usage = crate::MeteredUsage { cpu_fuel: cursor.u64()?, memory_bytes: cursor.u64()?, storage_read_bytes: cursor.u64()?, storage_write_bytes: cursor.u64()?,
            output_values: cursor.u32()?, output_bytes: cursor.u64()?, occupancy_byte_batches: cursor.u128()?, occupancy_fee_units: cursor.u128()?, fee_units: cursor.u128()? };
        if usage != meter.execution_trace_usage().map_err(|_| E::StateUnavailable)? || cursor.u64()? != meter.cpu_remaining() { return Err(E::Binding); }
        let failure_subtree_fuel = if cursor.boolean()? { Some(cursor.u64()?) } else { None };
        let legacy_reference_engine_committed = cursor.u64()?;
        let metering_schedule = crate::FuelSchedule::from_protocol_bytes(cursor.take(76)?).map_err(|_| E::Encoding)?;
        let legacy_reference_fuel = cursor.boolean()?;
        let protocol_context = if cursor.boolean()? { Some(ExecutionContext::from_untrusted_canonical_bytes(cursor.take(24)?)?) } else { None };
        let composition = match (graphs.composition, resolver) {
            (None, None) => None,
            (Some((revision, graph)), Some(resolver)) => Some(Composition::new(resolver, graph, revision)),
            _ => return Err(E::Binding),
        };
        let failure_graph = graphs.failure_graph;
        for graph in [composition.as_ref().map(Composition::graph), failure_graph.as_ref()] {
            let present = cursor.boolean()?;
            if present != graph.is_some() { return Err(E::Binding); }
            if let Some(graph) = graph {
                let length = cursor.usize64()?;
                if cursor.take(length)? != graph.canonical_evidence() { return Err(E::Binding); }
            }
        }
        let refusal = if cursor.boolean()? { Some(replay_composition_refusal(&mut cursor)?) } else { None };
        let outcome = match cursor.u8()? {
            0 => None,
            tag @ (1 | 2) => {
                let length = cursor.usize64()?;
                let encoded = cursor.take(length)?;
                Some(if tag == 1 { V2OutcomeRegion::Response(ResponseRegion::from_untrusted_canonical_bytes(encoded)?) }
                    else { V2OutcomeRegion::Failure(ProgramFailure::canonical_decode(encoded).map_err(|_| E::Encoding)?) })
            }
            _ => return Err(E::Encoding),
        };
        if !cursor.done() { return Err(E::Encoding); }
        let v2_host_identity = Some(abi.v2_host_state_identity(&pair.baseline).map_err(|_| E::StateUnavailable)?);
        let value = Self { meter, abi: Some(abi), composition, refusal, outcome, failure_subtree_fuel, failure_graph,
            protocol_context, metering_schedule, legacy_reference_fuel, legacy_reference_engine_committed, trace_storage_baseline: pair.baseline, v2_host_identity };
        let mut canonical = Vec::new(); let mut failure = None;
        value.write_v2_runtime_state(&commitment, &mut |part| append(&mut canonical, part, bytes.len()).map_err(|error| { failure = Some(error); AbiError::InvalidEncoding }))
            .map_err(|_| failure.unwrap_or(E::Encoding))?;
        if canonical != bytes { return Err(E::Encoding); }
        Ok(value)
    }
}


impl RuntimeState {
    pub(crate) fn replay_composition_witness(&self, code_hash: [u8; 32], maximum: usize) -> Result<crate::replay::CompositionReplayWitnessV1, crate::replay::ReplayWitnessError> {
        crate::replay::CompositionReplayWitnessV1::capture(code_hash, self.composition.as_ref(), self.failure_graph.as_ref(), maximum)
    }
}
