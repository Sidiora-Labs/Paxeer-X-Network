//! Candidate execution-context host-function registration.

use wasmi::core::{Trap, TrapCode};
use wasmi::{Caller, Linker};

use crate::abi::context::{ContextField, ContextRefusal, CONTEXT_FUEL_PER_BYTE};
use crate::abi::response::CANDIDATE_ABI_MODULE;
use crate::execute::ExecutionFault;

use super::memory::validate_output;
use super::{linker_fault, RuntimeState, STATUS_BOUNDS, STATUS_DENIED, STATUS_INVALID};

pub(super) fn register_v2(linker: &mut Linker<RuntimeState>) -> Result<(), ExecutionFault> {
    linker
        .func_wrap(
            CANDIDATE_ABI_MODULE,
            "context_read",
            |mut caller: Caller<'_, RuntimeState>,
             raw_field: i32,
             output_pointer: i32,
             output_capacity: i32|
             -> Result<i32, Trap> {
                let field = match ContextField::try_from(raw_field) {
                    Ok(field) => field,
                    Err(ContextRefusal::UnknownField) => return Ok(STATUS_INVALID),
                    Err(_) => return Ok(STATUS_DENIED),
                };
                let output = match validate_output(&caller, output_pointer, output_capacity) {
                    Ok(output) => output,
                    Err(status) => return Ok(status),
                };
                if super::reconcile_reference_guest_cpu(&mut caller).is_err() {
                    return Err(Trap::from(TrapCode::OutOfFuel));
                }
                let initial = match caller.data().context_field(field) {
                    Ok(value) => value,
                    Err(ContextRefusal::UnknownField) => return Ok(STATUS_INVALID),
                    Err(ContextRefusal::Unauthenticated | ContextRefusal::FrameMismatch) => {
                        return Ok(STATUS_DENIED)
                    }
                };
                if initial.len() > output.capacity() {
                    return Ok(STATUS_BOUNDS);
                }
                let fuel = u64::try_from(initial.len())
                    .ok()
                    .and_then(|bytes| bytes.checked_mul(CONTEXT_FUEL_PER_BYTE))
                    .ok_or_else(|| Trap::from(TrapCode::OutOfFuel))?;
                if super::charge_host_cpu(&mut caller, fuel).is_err() {
                    return Err(Trap::from(TrapCode::OutOfFuel));
                }
                let Ok(value) = caller.data().context_field(field) else {
                    return Ok(STATUS_DENIED);
                };
                if let Err(status) = output.write(&mut caller, &value) {
                    return Ok(status);
                }
                Ok(i32::try_from(value.len()).unwrap_or(STATUS_BOUNDS))
            },
        )
        .map_err(|error| linker_fault(&error))?;
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::STATUS_DENIED;
    use crate::abi::response::CANDIDATE_ABI_MODULE;
    use crate::test_support::{
        code_section, func_body, function_section, import_section, module, raw_section,
        type_section, unsigned_leb, OP_CALL, OP_END, OP_I32_CONST, TYPE_I32,
    };
    use crate::{
        AuthorizationContext, AuthorizedExecutionRequest, CapabilitySet, CompositionContext,
        Executor, PrincipalId, ProgramId, Storage, UnavailableReceiptOracle, V2ActivityOutcome,
        WasmEngine, CALL_ENTRY_EXPORT,
    };

    fn constant(code: &mut Vec<u8>, mut value: i32) {
        code.push(OP_I32_CONST);
        loop {
            let byte = (value & 0x7f) as u8;
            value >>= 7;
            if (value == 0 && byte & 0x40 == 0) || (value == -1 && byte & 0x40 != 0) {
                code.push(byte);
                return;
            }
            code.push(byte | 0x80);
        }
    }

    fn require_equal(code: &mut Vec<u8>, expected: i32) {
        constant(code, expected);
        code.extend([0x47, 0x04, 0x40, 0x00, OP_END]);
    }

    #[test]
    fn actual_guest_without_authenticated_context_refuses_every_field() {
        let mut exports = unsigned_leb(3);
        for (name, kind, index) in [
            ("layerx_reserve", 0_u8, 1_u8),
            (CALL_ENTRY_EXPORT, 0, 2),
            ("memory", 2, 0),
        ] {
            exports.extend(unsigned_leb(name.len() as u64));
            exports.extend_from_slice(name.as_bytes());
            exports.extend([kind, index]);
        }
        let mut entry = Vec::new();
        for field in 1..=9 {
            constant(&mut entry, field);
            constant(&mut entry, 0);
            constant(&mut entry, 33);
            entry.extend([OP_CALL, 0]);
            require_equal(&mut entry, STATUS_DENIED);
            for offset in 0..33 {
                constant(&mut entry, offset);
                entry.extend([0x2d, 0, 0]);
                require_equal(&mut entry, 0x5a);
            }
        }
        constant(&mut entry, 0);
        entry.push(OP_END);
        let mut reserve = Vec::new();
        constant(&mut reserve, 4096);
        reserve.push(OP_END);
        let mut data = vec![1, 0, OP_I32_CONST, 0, OP_END, 33];
        data.extend([0x5a; 33]);
        let bytes = module(&[
            type_section(&[
                (&[TYPE_I32, TYPE_I32, TYPE_I32], &[TYPE_I32]),
                (&[TYPE_I32], &[TYPE_I32]),
                (&[TYPE_I32, TYPE_I32], &[TYPE_I32]),
            ]),
            import_section(&[(CANDIDATE_ABI_MODULE, "context_read", 0)]),
            function_section(&[1, 2]),
            raw_section(5, &[1, 1, 1, 1]),
            raw_section(7, &exports),
            code_section(&[func_body(&[], &reserve), func_body(&[], &entry)]),
            raw_section(11, &data),
        ]);
        let validated = WasmEngine::declared()
            .unwrap_or_else(|error| panic!("engine: {error}"))
            .validate_candidate_v2(&bytes)
            .unwrap_or_else(|error| panic!("guest: {error}"));
        let program = ProgramId::new([0x31; 32]).expect("nonzero program");
        let principal = PrincipalId::new([0x32; 32]).expect("nonzero principal");
        let record = Executor::declared()
            .execute_authorized_v2(
                &mut Storage::default(),
                AuthorizedExecutionRequest {
                    module: &validated,
                    program,
                    authorization: AuthorizationContext::new(principal, CapabilitySet::empty()),
                    receipts: &UnavailableReceiptOracle,
                    entrypoint: CALL_ENTRY_EXPORT,
                    calldata: &[],
                    composition: CompositionContext::isolated(),
                    response_capacity: 0,
                },
            )
            .unwrap_or_else(|error| panic!("real guest execution: {error}"));
        assert!(matches!(
            record.outcome(),
            V2ActivityOutcome::Success { .. }
        ));
        assert!(record
            .effects()
            .expect("successful effects")
            .transfers
            .is_empty());
    }

    #[test]
    fn actual_graph_boundary_preserves_context_and_refuses_edge_sixty_five() {
        use crate::calls::{CallGraph, CompositionRefusal, CompositionRules};
        let root = ProgramId::new([1; 32]).expect("root");
        let principal = PrincipalId::new([2; 32]).expect("principal");
        let mut graph = CallGraph::root(CompositionRules::declared(), root, principal);
        assert_eq!(graph.immediate_caller(), None);
        for branch in 0_u8..8 {
            let parent = ProgramId::new([10 + branch; 32]).expect("parent");
            graph.enter(parent).expect("branch admitted");
            assert_eq!(graph.current().expect("parent frame").program(), parent);
            assert_eq!(
                graph.current().expect("parent frame").principal(),
                principal
            );
            assert_eq!(graph.immediate_caller(), Some(root));
            for child in 0_u8..7 {
                let leaf = ProgramId::new([30 + child; 32]).expect("leaf");
                graph.enter(leaf).expect("leaf admitted");
                assert_eq!(graph.current().expect("leaf frame").program(), leaf);
                assert_eq!(graph.current().expect("leaf frame").principal(), principal);
                assert_eq!(graph.immediate_caller(), Some(parent));
                graph.leave();
                assert_eq!(graph.current().expect("restored parent").program(), parent);
                assert_eq!(graph.immediate_caller(), Some(root));
            }
            if branch == 7 {
                assert_eq!(graph.edges().len(), 64);
                let before = graph.clone();
                let extra = ProgramId::new([99; 32]).expect("extra leaf");
                assert_eq!(
                    graph.enter(extra),
                    Err(CompositionRefusal::EdgesExceeded {
                        limit: 64,
                        attempted: 65
                    })
                );
                assert_eq!(graph, before);
                assert_eq!(graph.visits(extra), 0);
                assert_eq!(graph.current().expect("unchanged parent").program(), parent);
                assert_eq!(graph.immediate_caller(), Some(root));
            }
            graph.leave();
            assert_eq!(graph.current().expect("restored root").program(), root);
            assert_eq!(graph.immediate_caller(), None);
        }
        assert_eq!(graph.edges().len(), 64);
        assert_eq!(graph.principal(), principal);
    }
}
