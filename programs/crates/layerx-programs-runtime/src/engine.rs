//! Construction of the vendored WASM engine stripped to the deterministic subset.

use core::fmt::{self, Display};
use std::sync::Arc;

use wasmi::{Config, Engine, StackLimits};

use crate::host::{self, HostLinker};
use crate::limits::ValidationLimits;
use crate::validate::{self, AbiRevision, ValidatedModule, ValidationRefusal};

const INITIAL_VALUE_STACK_HEIGHT: u32 = 1_024;

/// A typed refusal produced while constructing a [`WasmEngine`].
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum EngineRefusal {
    /// The declared stack limits were rejected by the engine.
    StackConfiguration {
        /// The engine's reason for rejecting the stack limits.
        reason: String,
    },
    /// The immutable versioned host surface could not be registered.
    HostLinkerConstruction {
        /// The engine's reason for refusing the host definition.
        reason: String,
    },
}

impl Display for EngineRefusal {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::StackConfiguration { reason } => {
                write!(f, "stack configuration refused: {reason}")
            }
            Self::HostLinkerConstruction { reason } => {
                write!(f, "versioned host linker construction refused: {reason}")
            }
        }
    }
}

impl std::error::Error for EngineRefusal {}

/// The vendored WASM engine configured for the deterministic subset only:
/// no clocks, no networking, no filesystem, no floats, no threads and no
/// randomness are reachable from guest code.
#[derive(Debug)]
pub struct WasmEngine {
    engine: Engine,
    limits: ValidationLimits,
    linker: Arc<HostLinker>,
}

impl WasmEngine {
    /// Constructs the deterministic engine under the given declared limits.
    ///
    /// # Errors
    ///
    /// Returns [`EngineRefusal::StackConfiguration`] when the engine rejects
    /// the declared stack limits, or [`EngineRefusal::HostLinkerConstruction`]
    /// when an ABI host surface cannot be sealed.
    pub fn new(limits: ValidationLimits) -> Result<Self, EngineRefusal> {
        let initial_height = INITIAL_VALUE_STACK_HEIGHT.min(limits.max_value_stack_height());
        let stack_limits = StackLimits::new(
            initial_height as usize,
            limits.max_value_stack_height() as usize,
            limits.max_call_depth() as usize,
        )
        .map_err(|error| EngineRefusal::StackConfiguration {
            reason: error.to_string(),
        })?;
        let mut config = Config::default();
        config
            .set_stack_limits(stack_limits)
            .wasm_mutable_global(true)
            .wasm_sign_extension(true)
            .wasm_multi_value(true)
            .wasm_bulk_memory(true)
            .wasm_saturating_float_to_int(false)
            .wasm_reference_types(false)
            .wasm_tail_call(false)
            .wasm_extended_const(false)
            // Consensus CPU accounting is injected into the module and routed
            // through RuntimeState::Meter; engine fuel is not authoritative.
            .consume_fuel(false)
            .floats(false);
        let engine = Engine::new(&config);
        let linker = construct_host_linker(&engine)?;
        Ok(Self {
            engine,
            limits,
            linker,
        })
    }

    /// Constructs the deterministic engine under the declared default limits.
    ///
    /// # Errors
    ///
    /// Returns an [`EngineRefusal`] when the stack limits or an ABI host
    /// surface cannot be constructed.
    pub fn declared() -> Result<Self, EngineRefusal> {
        Self::new(ValidationLimits::declared())
    }

    /// Validates a legacy-v1/qualification module under historical metering schedule one.
    /// Consensus admission must use [`Self::validate_versioned_metered`] with
    /// the exact protocol-state schedule selected for the activity.
    ///
    /// # Errors
    ///
    /// Returns a [`ValidationRefusal`] naming the violated rule when the module
    /// exceeds a declared limit, carries a forbidden import, uses floating
    /// point or vector types or instructions, or fails engine validation.
    pub fn validate(&self, wasm: &[u8]) -> Result<ValidatedModule, ValidationRefusal> {
        validate::validate_module(self, wasm, AbiRevision::V1)
    }

    /// Compatibility spelling retained for one release; use [`Self::validate_v2`].
    ///
    /// # Errors
    ///
    /// Returns the same deterministic validation refusals as [`Self::validate_v2`].
    pub fn validate_candidate_v2(&self, wasm: &[u8]) -> Result<ValidatedModule, ValidationRefusal> {
        self.validate_v2(wasm)
    }

    /// Validates the frozen version-two ABI for qualification under historical
    /// metering schedule one. Consensus admission supplies its schedule explicitly.
    ///
    /// # Errors
    /// Returns deterministic validation refusals for violations of the frozen ABI-v2 rules.
    pub fn validate_v2(&self, wasm: &[u8]) -> Result<ValidatedModule, ValidationRefusal> {
        validate::validate_module(self, wasm, AbiRevision::V2)
    }

    /// Validates the frozen version-three ABI for qualification under historical
    /// metering schedule one. Consensus admission supplies its schedule explicitly.
    ///
    /// # Errors
    /// Returns deterministic validation refusals for violations of the frozen ABI-v3 rules.
    pub fn validate_v3(&self, wasm: &[u8]) -> Result<ValidatedModule, ValidationRefusal> {
        validate::validate_module(self, wasm, AbiRevision::V3)
    }

    /// Validates the version-four ABI for qualification under historical
    /// metering schedule one. Consensus admission supplies its schedule explicitly.
    ///
    /// # Errors
    /// Returns deterministic validation refusals for violations of the ABI-v4 rules.
    pub fn validate_v4(&self, wasm: &[u8]) -> Result<ValidatedModule, ValidationRefusal> {
        validate::validate_module(self, wasm, AbiRevision::V4)
    }

    /// Replays legacy schedule-one validation from the recorded ABI version.
    /// New protocol execution must use [`Self::validate_versioned_metered`].
    ///
    /// # Errors
    ///
    /// Returns a refusal for an unsupported ABI or invalid module.
    pub fn validate_versioned(
        &self,
        abi_version: u16,
        wasm: &[u8],
    ) -> Result<ValidatedModule, ValidationRefusal> {
        self.validate_versioned_metered(abi_version, wasm, crate::FuelSchedule::WASMI_0_31_2)
    }

    /// Validates and instruments under the exact protocol-resolved schedule.
    ///
    /// # Errors
    ///
    /// Returns a refusal for an unsupported ABI, invalid module, or failed metering injection.
    pub fn validate_versioned_metered(
        &self,
        abi_version: u16,
        wasm: &[u8],
        schedule: crate::FuelSchedule,
    ) -> Result<ValidatedModule, ValidationRefusal> {
        let revision = crate::abi_policy::abi_revision(abi_version)
            .map_err(|_| ValidationRefusal::UnsupportedAbiVersion { abi_version })?;
        validate::validate_module_metered(self, wasm, revision, schedule)
    }

    /// Applies deployment bounds after deterministic validation under the recorded ABI.
    ///
    /// # Errors
    ///
    /// Returns the existing typed validation refusal for invalid modules or
    /// statically excessive frame footprints and finite direct call chains.
    pub fn validate_deployment_versioned_metered(
        &self,
        abi_version: u16,
        wasm: &[u8],
        schedule: crate::FuelSchedule,
    ) -> Result<ValidatedModule, ValidationRefusal> {
        let module = self.validate_versioned_metered(abi_version, wasm, schedule)?;
        validate_deployment_bounds(module.meter_injection().instrumented_wasm(), self.limits)?;
        Ok(module)
    }

    /// Returns the declared validation limits of this engine.
    #[must_use]
    pub const fn limits(&self) -> ValidationLimits {
        self.limits
    }

    /// Returns how many times this engine built its versioned host linker.
    #[must_use]
    pub fn host_linker_construction_count(&self) -> usize {
        self.linker.construction_count()
    }

    /// Returns the frozen number of host functions registered in the linker.
    #[must_use]
    pub fn host_function_registration_count(&self) -> usize {
        self.linker.registered_function_count()
    }

    pub(crate) const fn inner(&self) -> &Engine {
        &self.engine
    }

    pub(crate) fn host_linker(&self) -> Arc<HostLinker> {
        Arc::clone(&self.linker)
    }
}

fn construct_host_linker(engine: &Engine) -> Result<Arc<HostLinker>, EngineRefusal> {
    host::linker(engine)
        .map(Arc::new)
        .map_err(|error| EngineRefusal::HostLinkerConstruction {
            reason: error.to_string(),
        })
}

fn deployment_bound_refusal(reason: &str) -> ValidationRefusal {
    ValidationRefusal::RejectedByEngine {
        reason: reason.to_owned(),
    }
}

fn deployment_parse_refusal(error: wasmparser_nostd::BinaryReaderError) -> ValidationRefusal {
    ValidationRefusal::MalformedModule {
        reason: error.to_string(),
    }
}

fn validate_deployment_bounds(
    wasm: &[u8],
    limits: ValidationLimits,
) -> Result<(), ValidationRefusal> {
    use std::collections::{BTreeMap, BTreeSet, VecDeque};
    use wasmparser_nostd::{Operator, Parser, ValidPayload, Validator};

    let mut validator = Validator::new();
    let mut allocations = wasmparser_nostd::FuncValidatorAllocations::default();
    let mut calls = BTreeMap::<u32, BTreeSet<u32>>::new();
    for payload in Parser::new(0).parse_all(wasm) {
        let payload = payload.map_err(deployment_parse_refusal)?;
        if let ValidPayload::Func(function, body) =
            validator.payload(&payload).map_err(deployment_parse_refusal)?
        {
            let index = function.index;
            let mut function = function.into_validator(allocations);
            let mut reader = body.get_binary_reader();
            function.read_locals(&mut reader).map_err(deployment_parse_refusal)?;
            if function.len_locals() > limits.max_value_stack_height() {
                return Err(deployment_bound_refusal("declared value stack height exceeded"));
            }
            let outgoing = calls.entry(index).or_default();
            while !reader.eof() {
                let offset = reader.original_position();
                let operator = reader.read_operator().map_err(deployment_parse_refusal)?;
                function.op(offset, &operator).map_err(deployment_parse_refusal)?;
                let footprint = u64::from(function.len_locals())
                    + u64::from(function.operand_stack_height());
                if footprint > u64::from(limits.max_value_stack_height()) {
                    return Err(deployment_bound_refusal("declared value stack height exceeded"));
                }
                if let Operator::Call { function_index } = operator {
                    outgoing.insert(function_index);
                }
            }
            function.finish(reader.original_position()).map_err(deployment_parse_refusal)?;
            allocations = function.into_allocations();
        }
    }
    let mut remaining = BTreeMap::<u32, usize>::new();
    let mut parents = BTreeMap::<u32, Vec<u32>>::new();
    let mut depths = BTreeMap::<u32, u32>::new();
    let mut ready = VecDeque::new();
    for (&index, outgoing) in &calls {
        let mut count = 0;
        for target in outgoing.iter().filter(|target| calls.contains_key(*target)) {
            parents.entry(*target).or_default().push(index);
            count += 1;
        }
        remaining.insert(index, count);
        depths.insert(index, 1);
        if count == 0 {
            ready.push_back(index);
        }
    }
    while let Some(index) = ready.pop_front() {
        let depth = depths[&index];
        if depth > limits.max_call_depth() {
            return Err(deployment_bound_refusal("declared direct call depth exceeded"));
        }
        if let Some(callers) = parents.get(&index) {
            for caller in callers {
                if let Some(parent_depth) = depths.get_mut(caller) {
                    *parent_depth = (*parent_depth).max(depth.saturating_add(1));
                }
                if let Some(count) = remaining.get_mut(caller) {
                    *count -= 1;
                    if *count == 0 {
                        ready.push_back(*caller);
                    }
                }
            }
        }
    }
    Ok(())
}

#[cfg(test)]
mod deployment_admission_tests {
    use super::WasmEngine;
    use crate::test_support::{
        code_section, export_section, func_body, function_section, module,
        type_section, unsigned_leb, OP_CALL, OP_END, TYPE_I32,
    };
    use crate::{ExecutionError, ExecutionFault, Executor, FuelSchedule, ValidationLimits};

    #[test]
    fn deployment_admission_calls_recorded_abis() -> Result<(), Box<dyn std::error::Error>> {
        let wasm = [
            0,97,115,109,1,0,0,0,1,12,2,96,2,127,127,1,127,96,1,127,1,127,
            3,3,2,0,1,5,3,1,0,1,7,41,3,
            11,b'l',b'a',b'y',b'e',b'r',b'x',b'_',b'c',b'a',b'l',b'l',0,0,
            14,b'l',b'a',b'y',b'e',b'r',b'x',b'_',b'r',b'e',b's',b'e',b'r',b'v',b'e',0,1,
            6,b'm',b'e',b'm',b'o',b'r',b'y',2,0,10,11,2,4,0,65,0,11,4,0,65,0,11,
        ];
        let engine = WasmEngine::declared()?;
        for abi in 1..=4 {
            let admitted = engine.validate_deployment_versioned_metered(
                abi, &wasm, FuelSchedule::WASMI_0_31_2,
            )?;
            assert!(admitted.supports_interface_entrypoint("layerx_call"));
            let mut instance = admitted.instantiate()?;
            assert_eq!(crate::entrypoint::invoke(&mut instance, "layerx_call", &[0; 4])?, 0);
        }
        Ok(())
    }

    #[test]
    fn deployment_admission_bounds_and_recursive_runtime_guard(
    ) -> Result<(), Box<dyn std::error::Error>> {
        let limits = ValidationLimits::new(1_048_576, 4096, 64, 8)?;
        let engine = WasmEngine::new(limits)?;
        let mut bodies = Vec::new();
        for index in 0..9_u32 {
            let instructions = if index == 8 {
                vec![OP_END]
            } else {
                let mut instructions = vec![OP_CALL];
                instructions.extend(unsigned_leb(u64::from(index + 1)));
                instructions.push(OP_END);
                instructions
            };
            bodies.push(func_body(&[], &instructions));
        }
        let deep = module(&[
            type_section(&[(&[], &[])]), function_section(&[0; 9]),
            code_section(&bodies),
        ]);
        assert!(engine.validate(&deep).is_ok());
        assert!(matches!(
            engine.validate_deployment_versioned_metered(1, &deep, FuelSchedule::WASMI_0_31_2),
            Err(crate::ValidationRefusal::RejectedByEngine { .. })
        ));
        let locals = module(&[
            type_section(&[(&[], &[])]), function_section(&[0]),
            code_section(&[func_body(&[(65, TYPE_I32)], &[OP_END])]),
        ]);
        assert!(matches!(
            engine.validate_deployment_versioned_metered(1, &locals, FuelSchedule::WASMI_0_31_2),
            Err(crate::ValidationRefusal::RejectedByEngine { .. })
        ));
        let mut operands = Vec::new();
        for _ in 0..65 {
            operands.extend([0x41, 0]);
        }
        operands.extend([0x1a; 65]);
        operands.push(OP_END);
        let stack = module(&[
            type_section(&[(&[], &[])]), function_section(&[0]),
            code_section(&[func_body(&[], &operands)]),
        ]);
        assert!(matches!(
            engine.validate_deployment_versioned_metered(1, &stack, FuelSchedule::WASMI_0_31_2),
            Err(crate::ValidationRefusal::RejectedByEngine { .. })
        ));
        let recursive = module(&[
            type_section(&[(&[], &[])]), function_section(&[0]),
            export_section(&[("recurse", 0)]),
            code_section(&[func_body(&[], &[OP_CALL, 0, OP_END])]),
        ]);
        let admitted = engine.validate_deployment_versioned_metered(
            1, &recursive, FuelSchedule::WASMI_0_31_2,
        )?;
        assert!(matches!(
            Executor::declared().execute(&admitted, "recurse", &[]),
            Err(ExecutionError::Fault(ExecutionFault::StackExhausted))
        ));
        Ok(())
    }
}
