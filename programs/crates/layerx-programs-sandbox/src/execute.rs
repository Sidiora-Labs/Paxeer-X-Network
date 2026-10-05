//! Lease-scoped execution through the ordinary Programs runtime.

use core::fmt::{self, Display};

use layerx_programs_runtime::{
    AbiError, AuthorizationContext, AuthorizedExecutionRecord, AuthorizedExecutionRequest,
    Capability, CapabilitySet, CodeHash, CompositionContext, EntrypointRefusal, ExecutionError,
    ExecutionFault, Executor, MeterRefusal, ReceiptOracle, ResourceBudget, ResourceKind, Storage,
    StorageNamespace, ValidatedModule,
};

use crate::{BoundKind, Lease, LeaseLimits, LeaseRefusal, LeaseState, LeaseUsage};

/// Authority derived entirely from immutable lease state.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct LeaseCapabilities {
    principal: layerx_programs_runtime::PrincipalId,
    namespace: StorageNamespace,
    grants: CapabilitySet,
}

impl LeaseCapabilities {
    /// Derives the only authority available to a sandbox image. The root host
    /// program may access its lease-principal namespace; no shared storage,
    /// transfer, balance, receipt, event, or callee authority is admitted.
    /// # Errors
    ///
    /// Returns a refusal when the lease namespace, execution principal or capability set is invalid.
    pub fn derive(lease: &Lease) -> Result<Self, SandboxRefusal> {
        let principal = lease
            .namespace()
            .execution_principal()
            .map_err(SandboxRefusal::Lease)?;
        let namespace = lease
            .namespace()
            .storage_namespace()
            .map_err(SandboxRefusal::Lease)?;
        let grants = CapabilitySet::new([Capability::StorageRead, Capability::StorageWrite])
            .map_err(SandboxRefusal::Capability)?;
        Ok(Self {
            principal,
            namespace,
            grants,
        })
    }

    #[must_use]
    pub const fn principal(&self) -> layerx_programs_runtime::PrincipalId {
        self.principal
    }

    #[must_use]
    pub const fn namespace(&self) -> StorageNamespace {
        self.namespace
    }

    #[must_use]
    pub const fn grants(&self) -> &CapabilitySet {
        &self.grants
    }

    fn authorization(&self) -> AuthorizationContext {
        AuthorizationContext::new(self.principal, self.grants.clone())
    }
}

/// Borrowed inputs for one ordinary Programs call made on behalf of a lease.
pub struct SandboxExecutionRequest<'a> {
    pub module: &'a ValidatedModule,
    pub receipts: &'a dyn ReceiptOracle,
    pub entrypoint: &'a str,
    pub calldata: &'a [u8],
    pub composition: CompositionContext,
    pub response_capacity: usize,
    pub observed_batch: u64,
}

/// Exact execution result and cumulative lease accounting to commit together.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SandboxExecutionRecord {
    pub execution: AuthorizedExecutionRecord,
    pub activity_usage: LeaseUsage,
    pub cumulative_usage: LeaseUsage,
    pub activity_fee_units: u128,
    pub cumulative_escrow_consumed: u128,
}

/// Typed sandbox refusal. Ceiling exhaustion is structurally distinct from a
/// guest/runtime program failure.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum SandboxRefusal {
    Lease(LeaseRefusal),
    Capability(AbiError),
    LeaseNotActive {
        state: LeaseState,
    },
    LeaseExpired {
        expiry: u64,
        observed: u64,
    },
    ImageMismatch {
        leased: CodeHash,
        supplied: CodeHash,
    },
    CeilingExhausted {
        bound: BoundKind,
        limit: u128,
        attempted: u128,
    },
    GrowthCeilingExhausted {
        memory_limit: u64,
        table_limit: u64,
    },
    Program(ExecutionError),
    AccountingOverflow {
        bound: BoundKind,
    },
}

impl Display for SandboxRefusal {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(formatter, "{self:?}")
    }
}

impl std::error::Error for SandboxRefusal {}

/// Executes one sandbox activity as an ordinary authorized Programs call. The
/// authority, the resource budget and the fee schedule all come from the lease:
/// the call may touch only the lease namespace, every resource is metered by
/// the production meter against what the lease has left, and the exact fee is
/// charged to the lease escrow. Storage is assigned only after the namespace
/// and escrow ceilings hold; any refusal leaves storage untouched.
///
/// # Errors
///
/// Returns a typed refusal for an inactive or expired lease, an image the lease
/// did not admit, an exhausted lease ceiling, or a program failure.
pub fn execute_in_lease(
    storage: &mut Storage,
    lease: &Lease,
    request: SandboxExecutionRequest<'_>,
) -> Result<SandboxExecutionRecord, SandboxRefusal> {
    if lease.state() != LeaseState::Active {
        return Err(SandboxRefusal::LeaseNotActive {
            state: lease.state(),
        });
    }
    if request.observed_batch < lease.opened_at() {
        return Err(SandboxRefusal::Lease(LeaseRefusal::InvalidSequence));
    }
    if request.observed_batch >= lease.expiry() {
        return Err(SandboxRefusal::LeaseExpired {
            expiry: lease.expiry(),
            observed: request.observed_batch,
        });
    }
    if request.module.code_hash() != lease.image_code_hash() {
        return Err(SandboxRefusal::ImageMismatch {
            leased: lease.image_code_hash(),
            supplied: request.module.code_hash(),
        });
    }
    let capabilities = LeaseCapabilities::derive(lease)?;
    let limits = lease.limits();
    let prior = lease.usage();
    let budget = remaining_budget(limits, prior)?;
    let mut held = storage.clone();
    let execution = Executor::new(budget, lease.fee_schedule())
        .execute_authorized(
            &mut held,
            AuthorizedExecutionRequest {
                module: request.module,
                program: lease.host_program(),
                authorization: capabilities.authorization(),
                receipts: request.receipts,
                entrypoint: request.entrypoint,
                calldata: request.calldata,
                composition: request.composition,
                response_capacity: request.response_capacity,
            },
        )
        .map_err(|error| classify(error, limits, prior))?;
    let metered = execution.execution.usage;
    let namespace_bytes = held
        .namespace_persistent_bytes(capabilities.namespace())
        .map_err(|_| SandboxRefusal::AccountingOverflow {
            bound: BoundKind::NamespaceBytes,
        })?;
    if namespace_bytes > limits.namespace_bytes {
        return Err(SandboxRefusal::CeilingExhausted {
            bound: BoundKind::NamespaceBytes,
            limit: u128::from(limits.namespace_bytes),
            attempted: u128::from(namespace_bytes),
        });
    }
    let activity_usage = LeaseUsage {
        cpu_fuel: metered.cpu_fuel,
        memory_bytes: metered.memory_bytes,
        storage_read_bytes: metered.storage_read_bytes,
        storage_write_bytes: metered.storage_write_bytes,
        output_values: u64::from(metered.output_values),
        output_bytes: metered.output_bytes,
        table_elements: 0,
        namespace_bytes,
    };
    let cumulative_usage = accumulate(prior, activity_usage)?;
    let cumulative_escrow_consumed = lease
        .escrow_consumed()
        .checked_add(metered.fee_units)
        .ok_or(SandboxRefusal::AccountingOverflow {
            bound: BoundKind::Escrow,
        })?;
    if cumulative_escrow_consumed > lease.escrow_amount() {
        return Err(SandboxRefusal::CeilingExhausted {
            bound: BoundKind::Escrow,
            limit: lease.escrow_amount(),
            attempted: cumulative_escrow_consumed,
        });
    }
    *storage = held;
    Ok(SandboxExecutionRecord {
        execution,
        activity_usage,
        cumulative_usage,
        activity_fee_units: metered.fee_units,
        cumulative_escrow_consumed,
    })
}

fn accumulate(prior: LeaseUsage, activity: LeaseUsage) -> Result<LeaseUsage, SandboxRefusal> {
    Ok(LeaseUsage {
        cpu_fuel: add(BoundKind::CpuFuel, prior.cpu_fuel, activity.cpu_fuel)?,
        memory_bytes: prior.memory_bytes.max(activity.memory_bytes),
        storage_read_bytes: add(
            BoundKind::StorageReadBytes,
            prior.storage_read_bytes,
            activity.storage_read_bytes,
        )?,
        storage_write_bytes: add(
            BoundKind::StorageWriteBytes,
            prior.storage_write_bytes,
            activity.storage_write_bytes,
        )?,
        output_values: add(
            BoundKind::OutputValues,
            prior.output_values,
            activity.output_values,
        )?,
        output_bytes: add(
            BoundKind::OutputBytes,
            prior.output_bytes,
            activity.output_bytes,
        )?,
        table_elements: prior.table_elements,
        namespace_bytes: activity.namespace_bytes,
    })
}

fn remaining_budget(
    limits: LeaseLimits,
    prior: LeaseUsage,
) -> Result<ResourceBudget, SandboxRefusal> {
    let output_values = remaining(
        BoundKind::OutputValues,
        limits.output_values,
        prior.output_values,
    )?;
    Ok(ResourceBudget::new_complete(
        remaining(BoundKind::CpuFuel, limits.cpu_fuel, prior.cpu_fuel)?,
        limits.memory_bytes,
        remaining(
            BoundKind::StorageReadBytes,
            limits.storage_read_bytes,
            prior.storage_read_bytes,
        )?,
        remaining(
            BoundKind::StorageWriteBytes,
            limits.storage_write_bytes,
            prior.storage_write_bytes,
        )?,
        u32::try_from(output_values).map_err(|_| SandboxRefusal::AccountingOverflow {
            bound: BoundKind::OutputValues,
        })?,
        remaining(
            BoundKind::OutputBytes,
            limits.output_bytes,
            prior.output_bytes,
        )?,
        u32::try_from(limits.table_elements).map_err(|_| SandboxRefusal::AccountingOverflow {
            bound: BoundKind::TableElements,
        })?,
    ))
}

fn remaining(bound: BoundKind, limit: u64, consumed: u64) -> Result<u64, SandboxRefusal> {
    limit
        .checked_sub(consumed)
        .ok_or(SandboxRefusal::CeilingExhausted {
            bound,
            limit: u128::from(limit),
            attempted: u128::from(consumed),
        })
}

fn add(bound: BoundKind, left: u64, right: u64) -> Result<u64, SandboxRefusal> {
    left.checked_add(right)
        .ok_or(SandboxRefusal::AccountingOverflow { bound })
}

fn classify(error: ExecutionError, limits: LeaseLimits, prior: LeaseUsage) -> SandboxRefusal {
    match error {
        ExecutionError::Resource(refusal)
        | ExecutionError::Abi(AbiError::Meter(refusal))
        | ExecutionError::Entrypoint(EntrypointRefusal::Resource(refusal))
        | ExecutionError::Fault(ExecutionFault::Resource { refusal }) => {
            meter_ceiling(refusal, limits, prior)
        }
        ExecutionError::Fault(ExecutionFault::OutOfFuel) => SandboxRefusal::CeilingExhausted {
            bound: BoundKind::CpuFuel,
            limit: u128::from(limits.cpu_fuel),
            attempted: u128::from(limits.cpu_fuel).saturating_add(1),
        },
        ExecutionError::Fault(ExecutionFault::GrowthLimited) => {
            SandboxRefusal::GrowthCeilingExhausted {
                memory_limit: limits.memory_bytes,
                table_limit: limits.table_elements,
            }
        }
        other => SandboxRefusal::Program(other),
    }
}

/// Restates one per-activity meter refusal against the whole lease: cumulative
/// resources report the lease-wide ceiling and the lease-wide attempted total.
fn meter_ceiling(refusal: MeterRefusal, limits: LeaseLimits, prior: LeaseUsage) -> SandboxRefusal {
    let bound = |resource| match resource {
        ResourceKind::Cpu => BoundKind::CpuFuel,
        ResourceKind::Memory => BoundKind::MemoryBytes,
        ResourceKind::StorageRead => BoundKind::StorageReadBytes,
        ResourceKind::StorageWrite => BoundKind::StorageWriteBytes,
        ResourceKind::StorageOccupancy => BoundKind::NamespaceBytes,
        ResourceKind::Output => BoundKind::OutputValues,
        ResourceKind::OutputBytes => BoundKind::OutputBytes,
    };
    match refusal {
        MeterRefusal::BudgetExceeded {
            resource,
            limit,
            attempted,
        } => {
            let (ceiling, consumed) = match resource {
                ResourceKind::Cpu => (limits.cpu_fuel, prior.cpu_fuel),
                ResourceKind::Memory => (limits.memory_bytes, 0),
                ResourceKind::StorageRead => (limits.storage_read_bytes, prior.storage_read_bytes),
                ResourceKind::StorageWrite => {
                    (limits.storage_write_bytes, prior.storage_write_bytes)
                }
                ResourceKind::StorageOccupancy => (limit, 0),
                ResourceKind::Output => (limits.output_values, prior.output_values),
                ResourceKind::OutputBytes => (limits.output_bytes, prior.output_bytes),
            };
            SandboxRefusal::CeilingExhausted {
                bound: bound(resource),
                limit: u128::from(ceiling),
                attempted: u128::from(consumed).saturating_add(u128::from(attempted)),
            }
        }
        MeterRefusal::CounterOverflow { resource } => SandboxRefusal::AccountingOverflow {
            bound: bound(resource),
        },
        MeterRefusal::FeeOverflow => SandboxRefusal::AccountingOverflow {
            bound: BoundKind::Escrow,
        },
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use layerx_programs_runtime::test_support::{
        code_section, func_body, function_section, import_section, module, raw_section,
        type_section, unsigned_leb, OP_CALL, OP_DROP, OP_END, OP_I32_CONST, TYPE_I32, TYPE_I64,
    };
    use layerx_programs_runtime::{
        AuthorizedExecutionRequest, Executor, PrincipalId, ReceiptView, Storage, WasmEngine,
        ABI_MODULE, CALL_ENTRY_EXPORT,
    };

    struct NoReceipts;

    impl ReceiptOracle for NoReceipts {
        fn verified_receipt(&self, _digest: [u8; 32]) -> Result<ReceiptView, AbiError> {
            Err(AbiError::ReceiptMismatch)
        }
    }

    fn lease(id: u8) -> Lease {
        Lease::request(
            crate::LeaseId::new([id; 32]).unwrap_or_else(|error| panic!("lease id: {error:?}")),
            PrincipalId::new([9; 32]).unwrap_or_else(|error| panic!("tenant: {error:?}")),
            layerx_programs_runtime::ProgramId::new([7; 32])
                .unwrap_or_else(|error| panic!("program: {error:?}")),
            [6; 32],
            [5; 32],
            1_000_000,
            crate::LeaseLimits {
                cpu_fuel: 100_000,
                memory_bytes: 65_536,
                storage_read_bytes: 1_024,
                storage_write_bytes: 1_024,
                output_values: 4,
                output_bytes: 1_024,
                table_elements: 1,
                namespace_bytes: 1_024,
            },
            1,
            100,
        )
        .unwrap_or_else(|error| panic!("lease: {error:?}"))
    }

    #[test]
    fn capabilities_are_derived_and_contain_no_escape_authority() {
        let lease = lease(1);
        let capabilities = LeaseCapabilities::derive(&lease)
            .unwrap_or_else(|error| panic!("capabilities: {error:?}"));
        assert_eq!(
            capabilities.principal(),
            lease
                .namespace()
                .execution_principal()
                .unwrap_or_else(|error| panic!("principal: {error:?}"))
        );
        assert_eq!(
            capabilities.namespace(),
            lease
                .namespace()
                .storage_namespace()
                .unwrap_or_else(|error| panic!("namespace: {error:?}"))
        );
        assert_eq!(capabilities.grants().canonical_encoding(), vec![0, 2, 1, 2]);
    }

    #[test]
    fn adjacent_leases_cannot_observe_the_same_runtime_namespace() {
        let left =
            LeaseCapabilities::derive(&lease(1)).unwrap_or_else(|error| panic!("left: {error:?}"));
        let right =
            LeaseCapabilities::derive(&lease(2)).unwrap_or_else(|error| panic!("right: {error:?}"));
        assert_ne!(left.principal(), right.principal());
        assert_ne!(left.namespace(), right.namespace());
    }

    #[test]
    fn hostile_authority_families_are_absent_by_construction() {
        let capabilities = LeaseCapabilities::derive(&lease(3))
            .unwrap_or_else(|error| panic!("capabilities: {error:?}"));
        let encoded = capabilities.grants().canonical_encoding();
        for hostile_tag in [3u8, 4, 5, 6, 7, 8, 9, 10] {
            assert!(!encoded[2..].contains(&hostile_tag));
        }
    }

    fn exports() -> Vec<u8> {
        let entries = [
            ("layerx_reserve", 0u8, 1u8),
            (CALL_ENTRY_EXPORT, 0, 2),
            ("memory", 2, 0),
        ];
        let mut payload = unsigned_leb(entries.len() as u64);
        for (name, kind, index) in entries {
            payload.extend(unsigned_leb(name.len() as u64));
            payload.extend_from_slice(name.as_bytes());
            payload.extend_from_slice(&[kind, index]);
        }
        raw_section(7, &payload)
    }

    fn hostile_host_call_image(function: &str, arity: usize) -> Vec<u8> {
        let params = vec![TYPE_I32; arity];
        let reserve_params = [TYPE_I32];
        let entry_params = [TYPE_I32, TYPE_I32];
        let result = [TYPE_I32];
        let mut entry = Vec::new();
        let arguments = if function == "program_call" {
            vec![0, 32, 0, 0, 32, 2]
        } else {
            vec![0; arity]
        };
        for argument in arguments {
            entry.extend([OP_I32_CONST, argument]);
        }
        let mut data = vec![1, 0, OP_I32_CONST, 0, OP_END, 32];
        data.extend([8; 32]);
        entry.extend([OP_CALL, 0, OP_END]);
        module(&[
            type_section(&[
                (params.as_slice(), result.as_slice()),
                (reserve_params.as_slice(), result.as_slice()),
                (entry_params.as_slice(), result.as_slice()),
            ]),
            import_section(&[(ABI_MODULE, function, 0)]),
            function_section(&[1, 2]),
            raw_section(5, &[1, 1, 1, 1]),
            exports(),
            code_section(&[
                func_body(&[], &[OP_I32_CONST, 0, OP_END]),
                func_body(&[], &entry),
            ]),
            raw_section(11, &data),
        ])
    }

    #[test]
    fn hostile_images_cannot_emit_or_call_an_unleased_program() {
        let lease = lease(4);
        let capabilities = LeaseCapabilities::derive(&lease)
            .unwrap_or_else(|error| panic!("capabilities: {error:?}"));
        let engine = WasmEngine::declared().unwrap_or_else(|error| panic!("engine: {error:?}"));
        for (function, arity) in [("event_emit", 4usize), ("program_call", 6usize)] {
            let module = engine
                .validate(&hostile_host_call_image(function, arity))
                .unwrap_or_else(|error| panic!("image: {error:?}"));
            let mut catalog = layerx_programs_runtime::ProgramCatalog::new();
            assert!(catalog
                .insert(
                    layerx_programs_runtime::ProgramId::new([8; 32])
                        .unwrap_or_else(|error| panic!("callee: {error:?}")),
                    engine
                        .validate(&hostile_host_call_image(function, arity))
                        .unwrap_or_else(|error| panic!("callee image: {error:?}"))
                )
                .is_none());
            let mut storage = Storage::new();
            let before = storage.clone();
            let result = Executor::declared().execute_authorized(
                &mut storage,
                AuthorizedExecutionRequest {
                    module: &module,
                    program: lease.host_program(),
                    authorization: capabilities.authorization(),
                    receipts: &NoReceipts,
                    entrypoint: CALL_ENTRY_EXPORT,
                    calldata: &[],
                    composition: CompositionContext::catalog(
                        catalog,
                        layerx_programs_runtime::CompositionRules::declared(),
                    ),
                    response_capacity: 0,
                },
            );
            let expected = if function == "event_emit" {
                ExecutionError::Entrypoint(
                    layerx_programs_runtime::EntrypointRefusal::GuestRefused { code: -1 },
                )
            } else {
                ExecutionError::Composition(layerx_programs_runtime::CompositionRefusal::Authority(
                    AbiError::CapabilityDenied,
                ))
            };
            assert_eq!(result, Err(expected));
            assert_eq!(storage, before);
        }
    }

    const OP_I64_CONST: u8 = 0x42;
    const KEY_AND_VALUE: [u8; 9] = [
        OP_I32_CONST,
        0,
        OP_I32_CONST,
        4,
        OP_I32_CONST,
        0,
        OP_I32_CONST,
        8,
        OP_CALL,
    ];
    const STORAGE_READ: [u8; 4] = [TYPE_I32; 4];

    fn limits() -> crate::LeaseLimits {
        crate::LeaseLimits {
            cpu_fuel: 100_000,
            memory_bytes: 65_536,
            storage_read_bytes: 1_024,
            storage_write_bytes: 1_024,
            output_values: 4,
            output_bytes: 1_024,
            table_elements: 1,
            namespace_bytes: 1_024,
        }
    }

    fn requested(id: u8, image: CodeHash, escrow: u128, limits: crate::LeaseLimits) -> Lease {
        Lease::request(
            crate::LeaseId::new([id; 32]).unwrap_or_else(|error| panic!("lease id: {error:?}")),
            PrincipalId::new([9; 32]).unwrap_or_else(|error| panic!("tenant: {error:?}")),
            layerx_programs_runtime::ProgramId::new([7; 32])
                .unwrap_or_else(|error| panic!("program: {error:?}")),
            image,
            [5; 32],
            escrow,
            limits,
            1,
            100,
        )
        .unwrap_or_else(|error| panic!("lease: {error:?}"))
    }

    fn active(id: u8, image: CodeHash, escrow: u128, limits: crate::LeaseLimits) -> Lease {
        let mut lease = requested(id, image, escrow, limits);
        lease
            .apply_host_activity(crate::LeaseActivity::Fund, [0x51; 32], 1)
            .unwrap_or_else(|error| panic!("fund: {error:?}"));
        lease
            .apply_host_activity(crate::LeaseActivity::Activate, [0x52; 32], 2)
            .unwrap_or_else(|error| panic!("activate: {error:?}"));
        assert_eq!(lease.state(), LeaseState::Active);
        lease
    }

    fn guest_exports(first_local: u8) -> Vec<u8> {
        let entries = [
            ("layerx_reserve", 0u8, first_local),
            (CALL_ENTRY_EXPORT, 0, first_local + 1),
            ("memory", 2, 0),
        ];
        let mut payload = unsigned_leb(entries.len() as u64);
        for (name, kind, index) in entries {
            payload.extend(unsigned_leb(name.len() as u64));
            payload.extend_from_slice(name.as_bytes());
            payload.extend_from_slice(&[kind, index]);
        }
        raw_section(7, &payload)
    }

    /// A one-page image whose memory starts with 32 bytes of 8, importing the
    /// named host functions (each returning i32) and running `entry`.
    fn guest(module_name: &str, imports: &[(&str, &[u8])], entry: &[u8]) -> Vec<u8> {
        let result = [TYPE_I32];
        let reserve_params = [TYPE_I32];
        let entry_params = [TYPE_I32, TYPE_I32];
        let mut types = imports
            .iter()
            .map(|(_, params)| (*params, result.as_slice()))
            .collect::<Vec<_>>();
        types.push((reserve_params.as_slice(), result.as_slice()));
        types.push((entry_params.as_slice(), result.as_slice()));
        let declared = imports
            .iter()
            .enumerate()
            .map(|(index, (name, _))| {
                (
                    module_name,
                    *name,
                    u32::try_from(index).unwrap_or_else(|error| panic!("index: {error}")),
                )
            })
            .collect::<Vec<_>>();
        let first_local = u8::try_from(imports.len()).unwrap_or_else(|error| panic!("{error}"));
        let mut body = entry.to_vec();
        body.push(OP_END);
        let mut data = vec![1, 0, OP_I32_CONST, 0, OP_END, 32];
        data.extend([8; 32]);
        module(&[
            type_section(&types),
            import_section(&declared),
            function_section(&[u32::from(first_local), u32::from(first_local) + 1]),
            raw_section(5, &[1, 1, 1, 1]),
            guest_exports(first_local),
            code_section(&[
                func_body(&[], &[OP_I32_CONST, 0, OP_END]),
                func_body(&[], &body),
            ]),
            raw_section(11, &data),
        ])
    }

    /// Reads the shared key into scratch memory, writes it with its value, and
    /// returns the read status: 0 when the key was absent from this lease's
    /// namespace, value length + 1 when present.
    fn probe() -> ValidatedModule {
        let mut entry = vec![
            OP_I32_CONST,
            0,
            OP_I32_CONST,
            4,
            OP_I32_CONST,
            32,
            OP_I32_CONST,
            16,
            OP_CALL,
            0,
        ];
        entry.extend(KEY_AND_VALUE);
        entry.extend([1, OP_DROP]);
        engine()
            .validate(&guest(
                ABI_MODULE,
                &[
                    ("storage_read", &STORAGE_READ),
                    ("storage_write", &STORAGE_READ),
                ],
                &entry,
            ))
            .unwrap_or_else(|error| panic!("probe: {error:?}"))
    }

    fn engine() -> WasmEngine {
        WasmEngine::declared().unwrap_or_else(|error| panic!("engine: {error:?}"))
    }

    fn sandbox_request(
        module: &ValidatedModule,
        observed_batch: u64,
    ) -> SandboxExecutionRequest<'_> {
        SandboxExecutionRequest {
            module,
            receipts: &NoReceipts,
            entrypoint: CALL_ENTRY_EXPORT,
            calldata: &[],
            composition: CompositionContext::isolated(),
            response_capacity: 0,
            observed_batch,
        }
    }

    fn ordinary(
        storage: &mut Storage,
        lease: &Lease,
        module: &ValidatedModule,
        budget: ResourceBudget,
    ) -> Result<AuthorizedExecutionRecord, ExecutionError> {
        let capabilities = LeaseCapabilities::derive(lease)
            .unwrap_or_else(|error| panic!("capabilities: {error:?}"));
        Executor::new(budget, lease.fee_schedule()).execute_authorized(
            storage,
            AuthorizedExecutionRequest {
                module,
                program: lease.host_program(),
                authorization: capabilities.authorization(),
                receipts: &NoReceipts,
                entrypoint: CALL_ENTRY_EXPORT,
                calldata: &[],
                composition: CompositionContext::isolated(),
                response_capacity: 0,
            },
        )
    }

    fn full_budget(limits: crate::LeaseLimits) -> ResourceBudget {
        remaining_budget(limits, LeaseUsage::default())
            .unwrap_or_else(|error| panic!("budget: {error:?}"))
    }

    fn lease_namespace(lease: &Lease) -> StorageNamespace {
        lease
            .namespace()
            .storage_namespace()
            .unwrap_or_else(|error| panic!("namespace: {error:?}"))
    }

    fn cell(storage: &Storage, namespace: StorageNamespace) -> Option<Vec<u8>> {
        storage
            .clone()
            .transaction(namespace)
            .read(&[8; 4])
            .unwrap_or_else(|error| panic!("read: {error:?}"))
    }

    fn seed(storage: &mut Storage, namespace: StorageNamespace, value: &[u8]) {
        let mut transaction = storage.transaction(namespace);
        transaction
            .write(&[8; 4], value)
            .unwrap_or_else(|error| panic!("seed: {error:?}"));
        assert_eq!(transaction.commit(), 1);
    }

    #[test]
    fn sandbox_call_is_ordinary_metered_execution_charged_to_the_lease() {
        let module = probe();
        let mut lease = active(10, module.code_hash(), 1_000_000, limits());
        let mut storage = Storage::new();
        let mut reference = storage.clone();
        let expected = ordinary(&mut reference, &lease, &module, full_budget(limits()))
            .unwrap_or_else(|error| panic!("ordinary call: {error:?}"));
        let first = execute_in_lease(&mut storage, &lease, sandbox_request(&module, 3))
            .unwrap_or_else(|error| panic!("first sandbox call: {error:?}"));
        assert_eq!(first.execution, expected);
        assert_eq!(storage, reference);
        assert_eq!(
            first.execution.execution.outputs,
            vec![layerx_programs_runtime::WasmValue::I32(0)]
        );
        let metered = expected.execution.usage;
        assert!(metered.cpu_fuel > 0 && metered.storage_read_bytes > 0);
        assert!(metered.storage_write_bytes > 0 && metered.fee_units > 0);
        let occupied = storage
            .namespace_persistent_bytes(lease_namespace(&lease))
            .unwrap_or_else(|error| panic!("occupancy: {error:?}"));
        assert!(occupied > 0);
        let activity = LeaseUsage {
            cpu_fuel: metered.cpu_fuel,
            memory_bytes: 65_536,
            storage_read_bytes: metered.storage_read_bytes,
            storage_write_bytes: metered.storage_write_bytes,
            output_values: 1,
            output_bytes: metered.output_bytes,
            table_elements: 0,
            namespace_bytes: occupied,
        };
        assert_eq!(metered.memory_bytes, 65_536);
        assert_eq!(first.activity_usage, activity);
        assert_eq!(first.cumulative_usage, activity);
        assert_eq!(first.activity_fee_units, metered.fee_units);
        assert_eq!(first.cumulative_escrow_consumed, metered.fee_units);
        assert_eq!(
            lease.record_usage(
                first.cumulative_usage,
                first.cumulative_escrow_consumed,
                3,
                None
            ),
            Ok(crate::UsageOutcome::Recorded(first.cumulative_usage))
        );

        let second = execute_in_lease(&mut storage, &lease, sandbox_request(&module, 4))
            .unwrap_or_else(|error| panic!("second sandbox call: {error:?}"));
        assert_eq!(
            second.execution.execution.outputs,
            vec![layerx_programs_runtime::WasmValue::I32(9)]
        );
        let charged = second.activity_usage;
        assert_eq!(
            second.cumulative_usage,
            LeaseUsage {
                cpu_fuel: first.cumulative_usage.cpu_fuel + charged.cpu_fuel,
                memory_bytes: 65_536,
                storage_read_bytes: first.cumulative_usage.storage_read_bytes
                    + charged.storage_read_bytes,
                storage_write_bytes: first.cumulative_usage.storage_write_bytes
                    + charged.storage_write_bytes,
                output_values: 2,
                output_bytes: first.cumulative_usage.output_bytes + charged.output_bytes,
                table_elements: 0,
                namespace_bytes: occupied,
            }
        );
        assert_eq!(
            second.cumulative_escrow_consumed,
            first.cumulative_escrow_consumed + second.activity_fee_units
        );
        assert_eq!(
            second.activity_fee_units,
            second.execution.execution.usage.fee_units
        );
    }

    #[test]
    fn sandbox_namespace_is_isolated_from_neighbour_tenant_and_shared_state() {
        let prober = probe();
        let neighbour = active(21, prober.code_hash(), 1_000_000, limits());
        let host = neighbour.host_program();
        let tenant = StorageNamespace::principal(host, neighbour.tenant());
        let shared = StorageNamespace::shared(host);
        let mut storage = Storage::new();
        seed(&mut storage, lease_namespace(&neighbour), b"neighbour");
        seed(&mut storage, tenant, b"tenant");
        seed(&mut storage, shared, b"shared");

        let mut owner = active(22, prober.code_hash(), 1_000_000, limits());
        let before = storage.clone();
        let wrote = execute_in_lease(&mut storage, &owner, sandbox_request(&prober, 3))
            .unwrap_or_else(|error| panic!("owner write: {error:?}"));
        assert_eq!(
            wrote.execution.execution.outputs,
            vec![layerx_programs_runtime::WasmValue::I32(0)]
        );
        assert_eq!(cell(&storage, lease_namespace(&owner)), Some(vec![8; 8]));
        for (namespace, value) in [
            (lease_namespace(&neighbour), b"neighbour".as_slice()),
            (tenant, b"tenant".as_slice()),
            (shared, b"shared".as_slice()),
        ] {
            assert_eq!(cell(&storage, namespace), Some(value.to_vec()));
        }
        let outside = |storage: &Storage| {
            storage
                .namespace_sizes()
                .unwrap_or_else(|error| panic!("sizes: {error:?}"))
                .into_iter()
                .filter(|(namespace, _)| *namespace != lease_namespace(&owner))
                .collect::<Vec<_>>()
        };
        assert_eq!(outside(&storage), outside(&before));
        assert!(wrote.execution.effects.events.is_empty());
        assert!(wrote.execution.effects.calls.is_empty());
        assert!(wrote.execution.effects.transfers.is_empty());
        assert_eq!(
            owner.record_usage(
                wrote.cumulative_usage,
                wrote.cumulative_escrow_consumed,
                3,
                None
            ),
            Ok(crate::UsageOutcome::Recorded(wrote.cumulative_usage))
        );

        let outsider = active(23, prober.code_hash(), 1_000_000, limits());
        let observed =
            execute_in_lease(&mut storage.clone(), &outsider, sandbox_request(&prober, 3))
                .unwrap_or_else(|error| panic!("outsider: {error:?}"));
        assert_eq!(
            observed.execution.execution.outputs,
            vec![layerx_programs_runtime::WasmValue::I32(0)]
        );
        let reread = execute_in_lease(&mut storage.clone(), &owner, sandbox_request(&prober, 4))
            .unwrap_or_else(|error| panic!("owner reread: {error:?}"));
        assert_eq!(
            reread.execution.execution.outputs,
            vec![layerx_programs_runtime::WasmValue::I32(9)]
        );
    }

    fn guest_refused() -> SandboxRefusal {
        SandboxRefusal::Program(ExecutionError::Entrypoint(
            layerx_programs_runtime::EntrypointRefusal::GuestRefused { code: -1 },
        ))
    }

    /// Images that try to emit, spend or read a receipt without a lease grant.
    fn ungranted_host_call_images(engine: &WasmEngine) -> Vec<(&'static str, ValidatedModule)> {
        let transfer_params = [TYPE_I64, TYPE_I64, TYPE_I32, TYPE_I32, TYPE_I32, TYPE_I32];
        let mut transfer_entry = vec![OP_I64_CONST, 0, OP_I64_CONST, 1];
        transfer_entry.extend([OP_I32_CONST, 0, OP_I32_CONST, 32]);
        transfer_entry.extend([OP_I32_CONST, 0, OP_I32_CONST, 32, OP_CALL, 0]);
        let mut receipt_entry = vec![OP_I32_CONST, 0, OP_I32_CONST, 32];
        receipt_entry.extend([OP_I32_CONST, 32, OP_I32_CONST, 16, OP_CALL, 0]);
        let images: [(&str, Vec<u8>); 3] = [
            ("emit an event", hostile_host_call_image("event_emit", 4)),
            (
                "spend from an account the lease does not own",
                guest(
                    ABI_MODULE,
                    &[("transfer_402", &transfer_params)],
                    &transfer_entry,
                ),
            ),
            (
                "read an unleased receipt",
                guest(
                    ABI_MODULE,
                    &[("receipt_read", &STORAGE_READ)],
                    &receipt_entry,
                ),
            ),
        ];
        images
            .into_iter()
            .map(|(attempt, image)| {
                let module = engine
                    .validate(&image)
                    .unwrap_or_else(|error| panic!("{attempt}: {error:?}"));
                (attempt, module)
            })
            .collect()
    }

    fn unleased_call_composition() -> CompositionContext {
        let mut catalog = layerx_programs_runtime::ProgramCatalog::new();
        assert!(catalog
            .insert(
                layerx_programs_runtime::ProgramId::new([8; 32])
                    .unwrap_or_else(|error| panic!("callee: {error:?}")),
                probe(),
            )
            .is_none());
        CompositionContext::catalog(
            catalog,
            layerx_programs_runtime::CompositionRules::declared(),
        )
    }

    fn shared_namespace_write_image(engine: &WasmEngine) -> ValidatedModule {
        let mut shared_write = vec![OP_I32_CONST, 2];
        shared_write.extend(KEY_AND_VALUE);
        shared_write.push(0);
        let shared_params = [TYPE_I32; 5];
        engine
            .validate_v2(&guest(
                layerx_programs_runtime::ABI_V2_MODULE,
                &[("storage_write_scoped", &shared_params)],
                &shared_write,
            ))
            .unwrap_or_else(|error| panic!("shared-write image: {error:?}"))
    }

    #[test]
    fn hostile_sandbox_image_escapes_are_refused_with_no_state_leaving_the_lease() {
        let engine = engine();
        let prober = probe();
        let mut storage = Storage::new();
        let neighbour = active(21, prober.code_hash(), 1_000_000, limits());
        seed(&mut storage, lease_namespace(&neighbour), b"neighbour");
        seed(
            &mut storage,
            StorageNamespace::shared(neighbour.host_program()),
            b"shared",
        );
        let mut attempts = ungranted_host_call_images(&engine)
            .into_iter()
            .map(|(attempt, module)| {
                (
                    attempt,
                    module,
                    CompositionContext::isolated(),
                    guest_refused(),
                )
            })
            .collect::<Vec<_>>();
        attempts.push((
            "call an unleased program",
            engine
                .validate(&hostile_host_call_image("program_call", 6))
                .unwrap_or_else(|error| panic!("call image: {error:?}")),
            unleased_call_composition(),
            SandboxRefusal::Program(ExecutionError::Composition(
                layerx_programs_runtime::CompositionRefusal::Authority(AbiError::CapabilityDenied),
            )),
        ));
        attempts.push((
            "write the host program's shared namespace",
            shared_namespace_write_image(&engine),
            CompositionContext::isolated(),
            SandboxRefusal::Program(ExecutionError::Abi(AbiError::WrongVersion)),
        ));
        for (index, (attempt, module, composition, expected)) in attempts.into_iter().enumerate() {
            let id = 40 + u8::try_from(index).unwrap_or_else(|error| panic!("{error}"));
            let lease = active(id, module.code_hash(), 1_000_000, limits());
            let leased = lease.clone();
            let mut held = storage.clone();
            let mut request = sandbox_request(&module, 3);
            request.composition = composition;
            assert_eq!(
                execute_in_lease(&mut held, &lease, request),
                Err(expected),
                "{attempt}"
            );
            assert_eq!(held, storage, "{attempt}");
            assert_eq!(lease, leased, "{attempt}");
        }

        let owner = active(22, prober.code_hash(), 1_000_000, limits());
        let foreign = engine
            .validate(&hostile_host_call_image("event_emit", 4))
            .unwrap_or_else(|error| panic!("foreign image: {error:?}"));
        let mut held = storage.clone();
        assert_eq!(
            execute_in_lease(&mut held, &owner, sandbox_request(&foreign, 4)),
            Err(SandboxRefusal::ImageMismatch {
                leased: prober.code_hash(),
                supplied: foreign.code_hash(),
            })
        );
        assert_eq!(held, storage);
    }

    fn exhausted(
        result: Result<SandboxExecutionRecord, SandboxRefusal>,
        bound: BoundKind,
        limit: u128,
    ) {
        match result {
            Err(SandboxRefusal::CeilingExhausted {
                bound: refused,
                limit: ceiling,
                attempted,
            }) => {
                assert_eq!((refused, ceiling), (bound, limit));
                assert!(attempted > ceiling, "{bound:?}: {attempted} <= {ceiling}");
            }
            other => panic!("{bound:?} was not a ceiling refusal: {other:?}"),
        }
    }

    #[test]
    fn ceiling_exhaustion_is_typed_and_distinct_from_program_failure() {
        let module = probe();
        let image = module.code_hash();
        let storage = Storage::new();

        let fee = ordinary(
            &mut storage.clone(),
            &requested(60, image, 1, limits()),
            &module,
            full_budget(limits()),
        )
        .unwrap_or_else(|error| panic!("ordinary call: {error:?}"))
        .execution
        .usage
        .fee_units;
        let mut held = storage.clone();
        assert_eq!(
            execute_in_lease(
                &mut held,
                &active(60, image, 1_000, limits()),
                sandbox_request(&module, 3)
            ),
            Err(SandboxRefusal::CeilingExhausted {
                bound: BoundKind::Escrow,
                limit: 1_000,
                attempted: fee,
            })
        );
        assert_eq!(held, storage);

        let cases = [
            (
                BoundKind::CpuFuel,
                crate::LeaseLimits {
                    cpu_fuel: 10,
                    ..limits()
                },
                10u128,
            ),
            (
                BoundKind::MemoryBytes,
                crate::LeaseLimits {
                    memory_bytes: 1_024,
                    ..limits()
                },
                1_024,
            ),
            (
                BoundKind::StorageWriteBytes,
                crate::LeaseLimits {
                    storage_write_bytes: 4,
                    ..limits()
                },
                4,
            ),
            (
                BoundKind::NamespaceBytes,
                crate::LeaseLimits {
                    namespace_bytes: 4,
                    ..limits()
                },
                4,
            ),
        ];
        for (index, (bound, bounded, limit)) in cases.into_iter().enumerate() {
            let id = 61 + u8::try_from(index).unwrap_or_else(|error| panic!("{error}"));
            let mut held = storage.clone();
            exhausted(
                execute_in_lease(
                    &mut held,
                    &active(id, image, 1_000_000, bounded),
                    sandbox_request(&module, 3),
                ),
                bound,
                limit,
            );
            assert_eq!(held, storage, "{bound:?}");
        }
    }

    #[test]
    fn cumulative_ceiling_refuses_a_later_run_instead_of_extending_the_lease() {
        let module = probe();
        let image = module.code_hash();
        let storage = Storage::new();
        let mut lease = active(
            70,
            image,
            1_000_000,
            crate::LeaseLimits {
                output_values: 1,
                ..limits()
            },
        );
        let mut held = storage.clone();
        let first = execute_in_lease(&mut held, &lease, sandbox_request(&module, 3))
            .unwrap_or_else(|error| panic!("first output: {error:?}"));
        assert_eq!(
            lease.record_usage(
                first.cumulative_usage,
                first.cumulative_escrow_consumed,
                3,
                None
            ),
            Ok(crate::UsageOutcome::Recorded(first.cumulative_usage))
        );
        let committed = held.clone();
        exhausted(
            execute_in_lease(&mut held, &lease, sandbox_request(&module, 4)),
            BoundKind::OutputValues,
            1,
        );
        assert_eq!(held, committed);
    }

    #[test]
    fn lease_state_and_program_failure_are_distinct_from_ceiling_exhaustion() {
        let module = probe();
        let image = module.code_hash();
        let storage = Storage::new();
        let mut held = storage.clone();
        assert_eq!(
            execute_in_lease(
                &mut held,
                &active(71, image, 1_000_000, limits()),
                sandbox_request(&module, 100)
            ),
            Err(SandboxRefusal::LeaseExpired {
                expiry: 100,
                observed: 100,
            })
        );
        assert_eq!(
            execute_in_lease(
                &mut held,
                &requested(72, image, 1_000_000, limits()),
                sandbox_request(&module, 3)
            ),
            Err(SandboxRefusal::LeaseNotActive {
                state: LeaseState::Requested,
            })
        );
        assert_eq!(held, storage);

        let failing = engine()
            .validate(&guest(ABI_MODULE, &[], &[OP_I32_CONST, 0x79]))
            .unwrap_or_else(|error| panic!("failing image: {error:?}"));
        let failure = execute_in_lease(
            &mut held,
            &active(73, failing.code_hash(), 1_000_000, limits()),
            sandbox_request(&failing, 3),
        );
        assert_eq!(
            failure,
            Err(SandboxRefusal::Program(ExecutionError::Entrypoint(
                layerx_programs_runtime::EntrypointRefusal::GuestRefused { code: -7 },
            )))
        );
        assert!(!matches!(
            failure,
            Err(SandboxRefusal::CeilingExhausted { .. }
                | SandboxRefusal::GrowthCeilingExhausted { .. })
        ));
        assert_eq!(held, storage);
    }
}
