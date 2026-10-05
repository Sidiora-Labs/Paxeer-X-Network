//! Market-step adjudication: the host judges one step of a provider's committed sandbox trace
//! for the calling market program, the way a contract reaches a precompile.

use layerx_program_sdk::arbiter::{MARKET_STEP_HEADER_CAPACITY, MARKET_STEP_OUTCOME_BYTES};
use wasmi::{Caller, Linker};

use crate::calls::{Composition, CompositionRefusal};
use crate::execute::{
    adjudicate_market_step, ExecutionFault, MarketStepRefusal, MAX_MARKET_STEP_EVIDENCE_BYTES,
};

use super::memory::{read_guest, validate_output};
use super::{
    linker_fault, RuntimeState, STATUS_BOUNDS, STATUS_DENIED, STATUS_EVIDENCE, STATUS_INVALID,
    STATUS_METER,
};

pub(super) fn register_v5(linker: &mut Linker<RuntimeState>) -> Result<(), ExecutionFault> {
    linker
        .func_wrap(
            crate::abi::manifest::ABI_V5_MODULE,
            "market_step_adjudicate",
            |mut caller: Caller<'_, RuntimeState>,
             header_pointer: i32,
             header_length: i32,
             evidence_pointer: i32,
             evidence_length: i32,
             output_pointer: i32,
             output_capacity: i32|
             -> i32 {
                let header = match read_guest(
                    &caller,
                    header_pointer,
                    header_length,
                    MARKET_STEP_HEADER_CAPACITY,
                ) {
                    Ok(bytes) => bytes,
                    Err(status) => return status,
                };
                let evidence = match read_guest(
                    &caller,
                    evidence_pointer,
                    evidence_length,
                    MAX_MARKET_STEP_EVIDENCE_BYTES,
                ) {
                    Ok(bytes) => bytes,
                    Err(status) => return status,
                };
                let output = match validate_output(&caller, output_pointer, output_capacity) {
                    Ok(output) => output,
                    Err(status) => return status,
                };
                if output.capacity() < MARKET_STEP_OUTCOME_BYTES {
                    return STATUS_BOUNDS;
                }
                let Some(program) = caller
                    .data()
                    .authorization_abi()
                    .map(crate::abi::Abi::program)
                else {
                    return STATUS_DENIED;
                };
                let Some(resolver) = caller.data().composition().map(Composition::resolver) else {
                    return STATUS_DENIED;
                };
                let fees = caller.data().meter().prices();
                let mut meter_refusal = None;
                let result = adjudicate_market_step(
                    &header,
                    &evidence,
                    program,
                    fees,
                    &*resolver,
                    &mut |fuel| {
                        super::charge_host_cpu(&mut caller, fuel).map_err(|refusal| {
                            meter_refusal = Some(refusal);
                            MarketStepRefusal::Meter
                        })
                    },
                );
                let outcome = match result {
                    Ok(outcome) => outcome,
                    Err(MarketStepRefusal::Meter) => {
                        if let Some(refusal) = meter_refusal {
                            caller
                                .data_mut()
                                .record_refusal(CompositionRefusal::Resource(refusal));
                        }
                        return STATUS_METER;
                    }
                    Err(MarketStepRefusal::Encoding) => return STATUS_INVALID,
                    Err(MarketStepRefusal::Denied) => return STATUS_DENIED,
                    Err(MarketStepRefusal::Evidence) => return STATUS_EVIDENCE,
                };
                if let Err(status) = output.write(&mut caller, &outcome) {
                    return status;
                }
                i32::try_from(outcome.len()).unwrap_or(STATUS_BOUNDS)
            },
        )
        .map_err(|error| linker_fault(&error))?;
    Ok(())
}
