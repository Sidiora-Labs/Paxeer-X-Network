//! Bounded successful response transport for the frozen ABI v2 revision.

use core::fmt::{self, Display};

use crate::meter::MeterRefusal;

use super::HostFunction;

/// Compatibility aliases for the frozen ABI-v2 response operations.
pub const CANDIDATE_ABI_MODULE: &str = super::manifest::ABI_V2_MODULE;
pub const CANDIDATE_ABI_MANIFEST: &str = super::manifest::ABI_V2_MANIFEST;

/// Compatibility alias for the complete frozen ABI-v2 host table.
pub const CANDIDATE_HOST_FUNCTIONS: [HostFunction; 19] = super::manifest::ABI_V2_HOST_FUNCTIONS;

/// Maximum successful response payload crossing one call boundary.
pub const MAX_CALL_RESPONSE_BYTES: usize = 1_048_576;

/// Owned response returned by one successful program invocation.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct CallResponse {
    pub code: i32,
    pub bytes: Vec<u8>,
}

/// Typed refusal while publishing or transporting a response.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum ResponseRefusal {
    TooLarge { bytes: usize, limit: usize },
    CapacityExceeded { bytes: usize, capacity: usize },
    DuplicatePublication,
    InvalidPublication,
    CodeMismatch { published: i32, returned: i32 },
    Meter(MeterRefusal),
}

impl Display for ResponseRefusal {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::TooLarge { bytes, limit } => {
                write!(formatter, "response size {bytes} exceeds limit {limit}")
            }
            Self::CapacityExceeded { bytes, capacity } => {
                write!(
                    formatter,
                    "response size {bytes} exceeds caller capacity {capacity}"
                )
            }
            Self::DuplicatePublication => formatter.write_str("response already published"),
            Self::InvalidPublication => {
                formatter.write_str("response publication region is invalid")
            }
            Self::CodeMismatch {
                published,
                returned,
            } => write!(
                formatter,
                "published response code {published} differs from returned code {returned}"
            ),
            Self::Meter(refusal) => Display::fmt(refusal, formatter),
        }
    }
}

impl std::error::Error for ResponseRefusal {}

#[derive(Clone, Debug, Eq, PartialEq)]
pub(crate) struct ResponseRegion {
    capacity: usize,
    published: Option<CallResponse>,
    refusal: Option<ResponseRefusal>,
}

impl ResponseRegion {
    pub(crate) fn canonical_state_len(&self) -> Result<u64, ()> {
        let payload = self
            .published
            .as_ref()
            .map_or(0, |response| response.bytes.len());
        let refusal = self
            .refusal
            .as_ref()
            .map_or(0, ResponseRefusal::canonical_len);
        8_u64
            .checked_add(1)
            .and_then(|value| {
                value.checked_add(if self.published.is_some() {
                    12_u64.checked_add(u64::try_from(payload).ok()?)?
                } else {
                    0
                })
            })
            .and_then(|value| value.checked_add(1))
            .and_then(|value| value.checked_add(u64::try_from(refusal).ok()?))
            .ok_or(())
    }

    pub(crate) fn canonical_state_write(&self, mut write: impl FnMut(&[u8])) {
        write(&(self.capacity as u64).to_be_bytes());
        match &self.published {
            None => write(&[0]),
            Some(response) => {
                write(&[1]);
                write(&response.code.to_be_bytes());
                write(&(response.bytes.len() as u64).to_be_bytes());
                write(&response.bytes);
            }
        }
        match &self.refusal {
            None => write(&[0]),
            Some(refusal) => {
                write(&[1]);
                refusal.canonical_write(write);
            }
        }
    }

    pub(crate) const fn has_publication(&self) -> bool {
        self.published.is_some() || self.refusal.is_some()
    }

    pub(crate) fn new(capacity: usize) -> Result<Self, ResponseRefusal> {
        if capacity > MAX_CALL_RESPONSE_BYTES {
            return Err(ResponseRefusal::TooLarge {
                bytes: capacity,
                limit: MAX_CALL_RESPONSE_BYTES,
            });
        }
        Ok(Self {
            capacity,
            published: None,
            refusal: None,
        })
    }

    pub(crate) fn publish(&mut self, response: CallResponse) -> Result<(), ResponseRefusal> {
        if let Some(refusal) = &self.refusal {
            return Err(refusal.clone());
        }
        let result = if self.published.is_some() {
            Err(ResponseRefusal::DuplicatePublication)
        } else if response.bytes.len() > MAX_CALL_RESPONSE_BYTES {
            Err(ResponseRefusal::TooLarge {
                bytes: response.bytes.len(),
                limit: MAX_CALL_RESPONSE_BYTES,
            })
        } else if response.bytes.len() > self.capacity {
            Err(ResponseRefusal::CapacityExceeded {
                bytes: response.bytes.len(),
                capacity: self.capacity,
            })
        } else {
            self.published = Some(response);
            Ok(())
        };
        if let Err(refusal) = &result {
            self.refusal = Some(refusal.clone());
        }
        result
    }

    pub(crate) fn refuse(&mut self, refusal: ResponseRefusal) {
        if self.refusal.is_none() {
            self.refusal = Some(refusal);
        }
    }

    pub(crate) fn finish(&self, returned: i32) -> Result<CallResponse, ResponseRefusal> {
        if let Some(refusal) = &self.refusal {
            return Err(refusal.clone());
        }
        let response = self.published.clone().unwrap_or(CallResponse {
            code: returned,
            bytes: Vec::new(),
        });
        if response.code != returned {
            return Err(ResponseRefusal::CodeMismatch {
                published: response.code,
                returned,
            });
        }
        Ok(response)
    }
}

impl ResponseRefusal {
    pub(crate) fn canonical_len(&self) -> usize {
        match self {
            Self::TooLarge { .. } | Self::CapacityExceeded { .. } => 17,
            Self::CodeMismatch { .. } => 9,
            Self::Meter(refusal) => 1 + meter_len(refusal),
            _ => 1,
        }
    }
    pub(crate) fn canonical_write(&self, mut write: impl FnMut(&[u8])) {
        match self {
            Self::TooLarge { bytes, limit } => {
                write(&[0]);
                write(&(*bytes as u64).to_be_bytes());
                write(&(*limit as u64).to_be_bytes());
            }
            Self::CapacityExceeded { bytes, capacity } => {
                write(&[1]);
                write(&(*bytes as u64).to_be_bytes());
                write(&(*capacity as u64).to_be_bytes());
            }
            Self::DuplicatePublication => write(&[2]),
            Self::InvalidPublication => write(&[3]),
            Self::CodeMismatch {
                published,
                returned,
            } => {
                write(&[4]);
                write(&published.to_be_bytes());
                write(&returned.to_be_bytes());
            }
            Self::Meter(refusal) => {
                write(&[5]);
                meter_write(refusal, write);
            }
        }
    }
}

fn resource_code(resource: crate::meter::ResourceKind) -> u8 {
    match resource {
        crate::meter::ResourceKind::Cpu => 0,
        crate::meter::ResourceKind::Memory => 1,
        crate::meter::ResourceKind::StorageRead => 2,
        crate::meter::ResourceKind::StorageWrite => 3,
        crate::meter::ResourceKind::StorageOccupancy => 4,
        crate::meter::ResourceKind::Output => 5,
        crate::meter::ResourceKind::OutputBytes => 6,
    }
}
fn meter_len(refusal: &MeterRefusal) -> usize {
    match refusal {
        MeterRefusal::BudgetExceeded { .. } => 18,
        MeterRefusal::CounterOverflow { .. } => 2,
        MeterRefusal::FeeOverflow => 1,
    }
}
fn meter_write(refusal: &MeterRefusal, mut write: impl FnMut(&[u8])) {
    match refusal {
        MeterRefusal::BudgetExceeded {
            resource,
            limit,
            attempted,
        } => {
            write(&[0, resource_code(*resource)]);
            write(&limit.to_be_bytes());
            write(&attempted.to_be_bytes());
        }
        MeterRefusal::CounterOverflow { resource } => write(&[1, resource_code(*resource)]),
        MeterRefusal::FeeOverflow => write(&[2]),
    }
}

impl ResponseRefusal {
    pub(crate) fn decode_untrusted_replay(
        cursor: &mut crate::replay::ReplayCursor<'_>,
    ) -> Result<Self, crate::replay::ReplayWitnessError> {
        use crate::replay::ReplayWitnessError as E;
        Ok(match cursor.u8()? {
            0 => Self::TooLarge {
                bytes: cursor.usize64()?,
                limit: cursor.usize64()?,
            },
            1 => Self::CapacityExceeded {
                bytes: cursor.usize64()?,
                capacity: cursor.usize64()?,
            },
            2 => Self::DuplicatePublication,
            3 => Self::InvalidPublication,
            4 => Self::CodeMismatch {
                published: cursor.i32()?,
                returned: cursor.i32()?,
            },
            5 => Self::Meter(crate::replay::decode_meter_refusal(cursor)?),
            _ => return Err(E::Encoding),
        })
    }
}
impl ResponseRegion {
    pub(crate) fn from_untrusted_canonical_bytes(
        bytes: &[u8],
    ) -> Result<Self, crate::replay::ReplayWitnessError> {
        use crate::replay::{ReplayCursor, ReplayWitnessError as E};
        let mut cursor = ReplayCursor::new(bytes);
        let capacity = cursor.usize64()?;
        if capacity > MAX_CALL_RESPONSE_BYTES {
            return Err(E::Bounds);
        }
        let published = if cursor.boolean()? {
            let code = cursor.i32()?;
            let length = cursor.usize64()?;
            if length > capacity || length > MAX_CALL_RESPONSE_BYTES {
                return Err(E::Bounds);
            }
            let mut value = Vec::new();
            crate::replay::append(&mut value, cursor.take(length)?, length)?;
            Some(CallResponse { code, bytes: value })
        } else {
            None
        };
        let refusal = if cursor.boolean()? {
            Some(ResponseRefusal::decode_untrusted_replay(&mut cursor)?)
        } else {
            None
        };
        if !cursor.done() {
            return Err(E::Encoding);
        }
        let value = Self {
            capacity,
            published,
            refusal,
        };
        let mut canonical = Vec::new();
        let mut failed = false;
        value.canonical_state_write(|part| {
            if crate::replay::append(&mut canonical, part, bytes.len()).is_err() {
                failed = true;
            }
        });
        if failed || canonical != bytes {
            return Err(E::Encoding);
        }
        Ok(value)
    }
}

#[cfg(test)]
mod tests {
    use super::{CallResponse, ResponseRefusal, ResponseRegion, MAX_CALL_RESPONSE_BYTES};

    fn canonical(region: &ResponseRegion) -> Vec<u8> {
        let mut bytes = Vec::new();
        region.canonical_state_write(|part| bytes.extend_from_slice(part));
        assert_eq!(
            region.canonical_state_len(),
            Ok(u64::try_from(bytes.len()).expect("canonical length"))
        );
        assert_eq!(
            ResponseRegion::from_untrusted_canonical_bytes(&bytes).as_ref(),
            Ok(region)
        );
        bytes
    }

    #[test]
    fn unpublished_region_returns_the_entry_code_with_empty_bytes() {
        let region = ResponseRegion::new(0).expect("zero capacity");
        assert_eq!(
            region.finish(4),
            Ok(CallResponse {
                code: 4,
                bytes: Vec::new(),
            })
        );
        canonical(&region);
    }

    #[test]
    fn exact_capacity_and_exact_maximum_publish_whole() {
        let mut region = ResponseRegion::new(3).expect("capacity");
        let response = CallResponse {
            code: 1,
            bytes: vec![0, 0xff, 7],
        };
        assert_eq!(region.publish(response.clone()), Ok(()));
        assert_eq!(region.finish(1), Ok(response));
        canonical(&region);

        let mut maximum = ResponseRegion::new(MAX_CALL_RESPONSE_BYTES).expect("maximum capacity");
        let response = CallResponse {
            code: 2,
            bytes: vec![0xa5; MAX_CALL_RESPONSE_BYTES],
        };
        assert_eq!(maximum.publish(response.clone()), Ok(()));
        assert_eq!(maximum.finish(2), Ok(response));
        assert_eq!(
            ResponseRegion::new(MAX_CALL_RESPONSE_BYTES + 1),
            Err(ResponseRefusal::TooLarge {
                bytes: MAX_CALL_RESPONSE_BYTES + 1,
                limit: MAX_CALL_RESPONSE_BYTES,
            })
        );
    }

    #[test]
    fn over_capacity_is_a_sticky_typed_refusal_never_a_truncation() {
        let mut region = ResponseRegion::new(3).expect("capacity");
        let refusal = ResponseRefusal::CapacityExceeded {
            bytes: 4,
            capacity: 3,
        };
        assert_eq!(
            region.publish(CallResponse {
                code: 1,
                bytes: vec![1, 2, 3, 4],
            }),
            Err(refusal.clone())
        );
        assert_eq!(
            region.publish(CallResponse {
                code: 1,
                bytes: vec![1, 2],
            }),
            Err(refusal.clone())
        );
        assert_eq!(region.finish(1), Err(refusal));
        let bytes = canonical(&region);
        assert_eq!(&bytes[..9], &[0, 0, 0, 0, 0, 0, 0, 3, 0]);
    }
}

#[cfg(test)]
mod transport_tests {
    use crate::abi::response::{ResponseRefusal, CANDIDATE_ABI_MODULE};
    use crate::calls::CompositionRefusal;
    use crate::test_support::{
        code_section, func_body, function_section, import_section, module, raw_section,
        type_section, unsigned_leb, OP_CALL, OP_DROP, OP_END, OP_I32_CONST, TYPE_I32, TYPE_I64,
    };
    use crate::{
        AuthorizationContext, AuthorizedExecutionRequest, Capability, CapabilitySet,
        CompositionContext, CompositionRules, ExecutionError, Executor, FeeSchedule, MeterRefusal,
        PrincipalId, ProgramCatalog, ProgramId, ResourceBudget, ResourceKind,
        UnavailableReceiptOracle, V2AuthorizedExecutionRecord, WasmEngine, CALL_ENTRY_EXPORT,
    };

    const SENTINEL: u8 = 0xee;
    const CAPABILITIES_POINTER: i32 = 1024;
    const RESULTS_POINTER: i32 = 2048;
    const OP_I64_STORE: u8 = 0x37;

    struct Edge {
        callee: usize,
        output: i32,
        capacity: i32,
    }

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

    fn exports(entries: &[(&str, u8, u8)]) -> Vec<u8> {
        let mut payload = unsigned_leb(entries.len() as u64);
        for (name, kind, index) in entries {
            payload.extend(unsigned_leb(name.len() as u64));
            payload.extend_from_slice(name.as_bytes());
            payload.extend([*kind, *index]);
        }
        raw_section(7, &payload)
    }

    fn data(segments: &[(i32, &[u8])]) -> Vec<u8> {
        let mut payload = unsigned_leb(segments.len() as u64);
        for (offset, bytes) in segments {
            payload.push(0);
            constant(&mut payload, *offset);
            payload.push(OP_END);
            payload.extend(unsigned_leb(bytes.len() as u64));
            payload.extend_from_slice(bytes);
        }
        raw_section(11, &payload)
    }

    fn reserve() -> Vec<u8> {
        let mut body = Vec::new();
        constant(&mut body, 0);
        body.push(OP_END);
        func_body(&[], &body)
    }

    fn responder(code: i32, published: Option<&[u8]>) -> Vec<u8> {
        let mut entry = Vec::new();
        if let Some(bytes) = published {
            constant(&mut entry, code);
            constant(&mut entry, 0);
            constant(
                &mut entry,
                i32::try_from(bytes.len()).expect("response length"),
            );
            entry.extend([OP_CALL, 0, OP_DROP]);
        }
        constant(&mut entry, code);
        entry.push(OP_END);
        let mut sections = vec![
            type_section(&[
                (&[TYPE_I32, TYPE_I32, TYPE_I32], &[TYPE_I32]),
                (&[TYPE_I32], &[TYPE_I32]),
                (&[TYPE_I32, TYPE_I32], &[TYPE_I32]),
            ]),
            import_section(&[(CANDIDATE_ABI_MODULE, "response_write", 0)]),
            function_section(&[1, 2]),
            raw_section(5, &[1, 1, 1, 1]),
            exports(&[
                ("layerx_reserve", 0, 1),
                (CALL_ENTRY_EXPORT, 0, 2),
                ("memory", 2, 0),
            ]),
            code_section(&[reserve(), func_body(&[], &entry)]),
        ];
        if let Some(bytes) = published.filter(|bytes| !bytes.is_empty()) {
            sections.push(data(&[(0, bytes)]));
        }
        module(&sections)
    }

    fn fanout_root(callees: &[ProgramId], edges: &[Edge], published: (i32, i32)) -> Vec<u8> {
        let mut entry = Vec::new();
        for (index, edge) in edges.iter().enumerate() {
            constant(
                &mut entry,
                RESULTS_POINTER + 8 * i32::try_from(index).expect("edge index"),
            );
            for value in [
                32 * i32::try_from(edge.callee).expect("callee index"),
                32,
                0,
                0,
                CAPABILITIES_POINTER,
                2,
                edge.output,
                edge.capacity,
            ] {
                constant(&mut entry, value);
            }
            entry.extend([OP_CALL, 1, OP_I64_STORE, 3, 0]);
        }
        constant(&mut entry, 0);
        constant(&mut entry, published.0);
        constant(&mut entry, published.1);
        entry.extend([OP_CALL, 0, OP_DROP]);
        constant(&mut entry, 0);
        entry.push(OP_END);
        let identities: Vec<u8> = callees.iter().flat_map(|program| program.bytes()).collect();
        let sentinels = vec![
            SENTINEL;
            edges
                .iter()
                .map(|edge| usize::try_from(edge.capacity).expect("capacity"))
                .sum()
        ];
        let first_output = edges.first().map_or(0, |edge| edge.output);
        module(&[
            type_section(&[
                (&[TYPE_I32, TYPE_I32, TYPE_I32], &[TYPE_I32]),
                (&[TYPE_I32; 8], &[TYPE_I64]),
                (&[TYPE_I32], &[TYPE_I32]),
                (&[TYPE_I32, TYPE_I32], &[TYPE_I32]),
            ]),
            import_section(&[
                (CANDIDATE_ABI_MODULE, "response_write", 0),
                (CANDIDATE_ABI_MODULE, "program_call_response", 1),
            ]),
            function_section(&[2, 3]),
            raw_section(5, &[1, 1, 1, 1]),
            exports(&[
                ("layerx_reserve", 0, 2),
                (CALL_ENTRY_EXPORT, 0, 3),
                ("memory", 2, 0),
            ]),
            code_section(&[reserve(), func_body(&[], &entry)]),
            data(&[
                (0, &identities),
                (CAPABILITIES_POINTER, &[0, 0]),
                (first_output, &sentinels),
            ]),
        ])
    }

    fn execute(
        root: &[u8],
        children: &[(ProgramId, Vec<u8>)],
        output_bytes: u64,
        response_capacity: usize,
    ) -> Result<V2AuthorizedExecutionRecord, ExecutionError> {
        let engine = WasmEngine::declared().expect("engine");
        let mut catalog = ProgramCatalog::new();
        let mut grants = Vec::new();
        for (program, wasm) in children {
            catalog.insert(
                *program,
                engine
                    .validate_candidate_v2(wasm)
                    .expect("child validation"),
            );
            grants.push(Capability::Call { program: *program });
        }
        let root = engine.validate_candidate_v2(root).expect("root validation");
        Executor::new(
            ResourceBudget::declared().with_output_bytes(output_bytes),
            FeeSchedule::declared(),
        )
        .execute_authorized_candidate(
            &mut Default::default(),
            AuthorizedExecutionRequest {
                module: &root,
                program: ProgramId::new([0x40; 32]).expect("root program"),
                authorization: AuthorizationContext::new(
                    PrincipalId::new([0x41; 32]).expect("principal"),
                    CapabilitySet::new(grants).expect("call grants"),
                ),
                receipts: &UnavailableReceiptOracle,
                entrypoint: CALL_ENTRY_EXPORT,
                calldata: &[],
                composition: CompositionContext::catalog(catalog, CompositionRules::declared()),
                response_capacity,
            },
        )
    }

    fn packed(bytes: &[u8], edge: usize) -> u64 {
        let word: [u8; 8] = bytes[edge * 8..edge * 8 + 8]
            .try_into()
            .expect("packed result word");
        u64::from_le_bytes(word)
    }

    #[test]
    fn sibling_fanout_reads_only_its_own_edge_response_and_meters_each_copy() {
        let first = ProgramId::new([0x51; 32]).expect("first callee");
        let silent = ProgramId::new([0x52; 32]).expect("silent callee");
        let last = ProgramId::new([0x53; 32]).expect("last callee");
        let first_bytes = [0xa1, 0x00, 0xa3];
        let last_bytes = [0xc1];
        let edges = [
            Edge {
                callee: 0,
                output: RESULTS_POINTER + 24,
                capacity: 4,
            },
            Edge {
                callee: 1,
                output: RESULTS_POINTER + 28,
                capacity: 4,
            },
            Edge {
                callee: 2,
                output: RESULTS_POINTER + 32,
                capacity: 1,
            },
        ];
        let published = 24 + 4 + 4 + 1;
        let root = fanout_root(&[first, silent, last], &edges, (RESULTS_POINTER, published));
        let children = [
            (first, responder(5, Some(&first_bytes))),
            (silent, responder(6, None)),
            (last, responder(7, Some(&last_bytes))),
        ];
        let capacity = usize::try_from(published).expect("published length");
        let record =
            execute(&root, &children, u64::MAX, capacity).expect("fan-out response activity");
        let response = record.response().expect("root success response");
        assert_eq!(response.code, 0);
        assert_eq!(response.bytes.len(), capacity);
        assert_eq!(packed(&response.bytes, 0), (5 << 32) | 3);
        assert_eq!(packed(&response.bytes, 1), 6 << 32);
        assert_eq!(packed(&response.bytes, 2), (7 << 32) | 1);
        assert_eq!(&response.bytes[24..28], &[0xa1, 0x00, 0xa3, SENTINEL]);
        assert_eq!(&response.bytes[28..32], &[SENTINEL; 4]);
        assert_eq!(&response.bytes[32..33], &last_bytes);
        let callees: Vec<ProgramId> = record
            .call_graph()
            .edges()
            .iter()
            .map(|edge| edge.callee())
            .collect();
        assert_eq!(callees, vec![first, silent, last]);
        let metered = (first_bytes.len() + last_bytes.len() + capacity) as u64;
        assert_eq!(record.execution().usage().output_bytes, metered);

        assert_eq!(
            execute(&root, &children, metered - 1, capacity),
            Err(ExecutionError::Resource(MeterRefusal::BudgetExceeded {
                resource: ResourceKind::OutputBytes,
                limit: metered - 1,
                attempted: metered,
            }))
        );
    }

    #[test]
    fn nested_response_past_the_caller_capacity_fails_typed_without_truncation() {
        let callee = ProgramId::new([0x61; 32]).expect("callee");
        let bytes = [1, 2, 3, 4];
        let children = [(callee, responder(9, Some(&bytes)))];
        let exact = fanout_root(
            &[callee],
            &[Edge {
                callee: 0,
                output: RESULTS_POINTER + 8,
                capacity: 4,
            }],
            (RESULTS_POINTER, 12),
        );
        let record = execute(&exact, &children, u64::MAX, 12).expect("exact capacity");
        let response = record.response().expect("exact response");
        assert_eq!(packed(&response.bytes, 0), (9 << 32) | 4);
        assert_eq!(&response.bytes[8..12], &bytes);

        let over = fanout_root(
            &[callee],
            &[Edge {
                callee: 0,
                output: RESULTS_POINTER + 8,
                capacity: 3,
            }],
            (RESULTS_POINTER, 11),
        );
        assert_eq!(
            execute(&over, &children, u64::MAX, 11),
            Err(ExecutionError::Composition(CompositionRefusal::Response(
                ResponseRefusal::CapacityExceeded {
                    bytes: 4,
                    capacity: 3,
                }
            )))
        );
    }
}
