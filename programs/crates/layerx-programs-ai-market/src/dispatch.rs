#![allow(non_upper_case_globals)]
//! Fixed selector routing and the native call router over the landed feature transitions.
use crate::codec::{self, ApplicationResult, ResultStatus, ValidatedEnvelope};
use crate::errors::{
    CodecResult, NON_CANONICAL, NOT_FOUND, UNKNOWN_OPERATION, WRONG_DOMAIN, WRONG_PROGRAM,
};
use crate::evaluators::{admission as challenge, authority};
use crate::registry::F01_SECTION_CAP;
use crate::registry_ops::{self, CallContext, PolicySection};
use crate::state::{self, RetainedResult, Section, SharedState};
use crate::types::{Presence, RequestDigest};
use crate::{commit_reveal, epoch, evidence, tasks, workers, MAX_STATE_BYTES};
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum SequencePolicy {
    Role,
    ObjectLocal,
}
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum CallBoundary {
    Mutation,
    ProgramRead,
}
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Operation(u16);
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct OperationMetadata {
    pub operation: Operation,
    pub name: &'static str,
    pub feature: u8,
    pub boundary: CallBoundary,
    pub sequence: SequencePolicy,
    pub delegate_allowed: bool,
    pub payload_min: usize,
    pub payload_max: usize,
}
impl Operation {
    /// Decodes a selector from the frozen operation table.
    ///
    /// # Errors
    /// Returns `UNKNOWN_OPERATION` when the selector is not in the table.
    pub fn decode(selector: u16) -> CodecResult<Self> {
        OPERATIONS
            .iter()
            .find(|m| m.operation.0 == selector)
            .map(|m| m.operation)
            .ok_or(UNKNOWN_OPERATION)
    }
    #[must_use]
    pub const fn selector(self) -> u16 {
        self.0
    }
    #[must_use]
    pub fn metadata(self) -> &'static OperationMetadata {
        // The private constructor and static table make this branch unreachable.
        for m in OPERATIONS {
            if m.operation == self {
                return m;
            }
        }
        unreachable!("Operation is constructed only by the frozen table")
    }
    /// Checks a payload length against the operation's bounds.
    ///
    /// # Errors
    /// Returns `NON_CANONICAL` when the length is outside the operation's bounds, or is neither 40 nor 48 for `0x0106`/`0x0107`.
    pub fn validate_payload_length(self, length: usize) -> CodecResult<()> {
        let m = self.metadata();
        if matches!(self.0, 0x0106 | 0x0107) && length != 40 && length != 48 {
            return Err(NON_CANONICAL);
        }
        if length < m.payload_min || length > m.payload_max {
            Err(NON_CANONICAL)
        } else {
            Ok(())
        }
    }
}
pub const CREATE: Operation = Operation(0x0101);
pub const STAGE_POLICY: Operation = Operation(0x0102);
pub const CANCEL_POLICY: Operation = Operation(0x0103);
pub const SCHEDULE_ACTIVATION: Operation = Operation(0x0104);
pub const ADVANCE_ACTIVATION: Operation = Operation(0x0105);
pub const SUSPEND: Operation = Operation(0x0106);
pub const UPDATE_METADATA: Operation = Operation(0x0107);
pub const APPOINT_OPERATOR: Operation = Operation(0x0108);
pub const REVOKE_OPERATOR: Operation = Operation(0x0109);
pub const REQUEST_CLOSE: Operation = Operation(0x010A);
pub const ADVANCE_CLOSE: Operation = Operation(0x010B);
pub const ADMIT_TASK: Operation = Operation(0x010C);
pub const ACCEPT_TASK: Operation = Operation(0x010D);
pub const CANCEL_TASK: Operation = Operation(0x010E);
pub const COMMIT_TASK_RESULT: Operation = Operation(0x010F);
pub const SEAL_TASK_SET: Operation = Operation(0x0110);
pub const OPEN_EPOCH: Operation = Operation(0x0111);
pub const EnrollWorker: Operation = Operation(0x0201);
pub const PublishMetadata: Operation = Operation(0x0202);
pub const SetDraining: Operation = Operation(0x0203);
pub const UndoDrain: Operation = Operation(0x0204);
pub const RotateDelegate: Operation = Operation(0x0205);
pub const RevokeDelegate: Operation = Operation(0x0206);
pub const RetireWorker: Operation = Operation(0x0207);
pub const AcceptEnrollment: Operation = Operation(0x0209);
pub const ExpireEnrollment: Operation = Operation(0x020A);
pub const ScheduleEvaluator: Operation = Operation(0x0301);
pub const RotateEvaluatorKey: Operation = Operation(0x0302);
pub const RevokeEvaluator: Operation = Operation(0x0303);
pub const ChallengeAssessment: Operation = Operation(0x0304);
pub const CommitScore: Operation = Operation(0x0401);
pub const RevealScore: Operation = Operation(0x0402);
pub const BeginAggregation: Operation = Operation(0x0501);
pub const ProcessAggregation: Operation = Operation(0x0502);
pub const FinalizeAggregation: Operation = Operation(0x0503);
pub const FUND: Operation = Operation(0x0601);
pub const CLAIM: Operation = Operation(0x0602);
pub const EXPIRE_EPOCH_CLAIMS: Operation = Operation(0x0603);
pub const REFUND_FREE: Operation = Operation(0x0604);
pub const PRUNE_EPOCH: Operation = Operation(0x0605);
pub const ResetHistory: Operation = Operation(0x0701);
pub const SuspendHistory: Operation = Operation(0x0702);
pub const ResumeHistory: Operation = Operation(0x0703);
pub const ApproveAdmission: Operation = Operation(0x0801);
pub const RevokeAdmissionApproval: Operation = Operation(0x0802);
pub const AdmitWorker: Operation = Operation(0x0803);
pub const AdmitEvaluator: Operation = Operation(0x0804);
pub const Heartbeat: Operation = Operation(0x0805);
pub const RequestExit: Operation = Operation(0x0806);
pub const CancelExit: Operation = Operation(0x0807);
pub const PruneInactive: Operation = Operation(0x0808);
pub const AdministrativeRemove: Operation = Operation(0x0809);
pub const SealEvidence: Operation = Operation(0x0901);
pub const READ_HEADER: Operation = Operation(0x0A01);
pub const READ_STATE_CHUNK: Operation = Operation(0x0A02);
#[allow(non_upper_case_globals)]
pub const OPERATIONS: &[OperationMetadata] = &[
    OperationMetadata {
        operation: CREATE,
        name: "CREATE",
        feature: 1,
        boundary: CallBoundary::Mutation,
        sequence: SequencePolicy::Role,
        delegate_allowed: false,
        payload_min: 0,
        payload_max: 15965,
    },
    OperationMetadata {
        operation: STAGE_POLICY,
        name: "STAGE_POLICY",
        feature: 1,
        boundary: CallBoundary::Mutation,
        sequence: SequencePolicy::Role,
        delegate_allowed: false,
        payload_min: 0,
        payload_max: 15965,
    },
    OperationMetadata {
        operation: CANCEL_POLICY,
        name: "CANCEL_POLICY",
        feature: 1,
        boundary: CallBoundary::Mutation,
        sequence: SequencePolicy::Role,
        delegate_allowed: false,
        payload_min: 16,
        payload_max: 16,
    },
    OperationMetadata {
        operation: SCHEDULE_ACTIVATION,
        name: "SCHEDULE_ACTIVATION",
        feature: 1,
        boundary: CallBoundary::Mutation,
        sequence: SequencePolicy::Role,
        delegate_allowed: false,
        payload_min: 16,
        payload_max: 16,
    },
    OperationMetadata {
        operation: ADVANCE_ACTIVATION,
        name: "ADVANCE_ACTIVATION",
        feature: 1,
        boundary: CallBoundary::Mutation,
        sequence: SequencePolicy::ObjectLocal,
        delegate_allowed: false,
        payload_min: 16,
        payload_max: 16,
    },
    OperationMetadata {
        operation: SUSPEND,
        name: "SUSPEND",
        feature: 1,
        boundary: CallBoundary::Mutation,
        sequence: SequencePolicy::Role,
        delegate_allowed: false,
        payload_min: 40,
        payload_max: 48,
    },
    OperationMetadata {
        operation: UPDATE_METADATA,
        name: "UPDATE_METADATA",
        feature: 1,
        boundary: CallBoundary::Mutation,
        sequence: SequencePolicy::Role,
        delegate_allowed: false,
        payload_min: 40,
        payload_max: 48,
    },
    OperationMetadata {
        operation: APPOINT_OPERATOR,
        name: "APPOINT_OPERATOR",
        feature: 1,
        boundary: CallBoundary::Mutation,
        sequence: SequencePolicy::Role,
        delegate_allowed: false,
        payload_min: 49,
        payload_max: 49,
    },
    OperationMetadata {
        operation: REVOKE_OPERATOR,
        name: "REVOKE_OPERATOR",
        feature: 1,
        boundary: CallBoundary::Mutation,
        sequence: SequencePolicy::Role,
        delegate_allowed: false,
        payload_min: 16,
        payload_max: 16,
    },
    OperationMetadata {
        operation: REQUEST_CLOSE,
        name: "REQUEST_CLOSE",
        feature: 1,
        boundary: CallBoundary::Mutation,
        sequence: SequencePolicy::Role,
        delegate_allowed: false,
        payload_min: 40,
        payload_max: 40,
    },
    OperationMetadata {
        operation: ADVANCE_CLOSE,
        name: "ADVANCE_CLOSE",
        feature: 1,
        boundary: CallBoundary::Mutation,
        sequence: SequencePolicy::ObjectLocal,
        delegate_allowed: false,
        payload_min: 11,
        payload_max: 11,
    },
    OperationMetadata {
        operation: ADMIT_TASK,
        name: "ADMIT_TASK",
        feature: 1,
        boundary: CallBoundary::Mutation,
        sequence: SequencePolicy::ObjectLocal,
        delegate_allowed: false,
        payload_min: 248,
        payload_max: 248,
    },
    OperationMetadata {
        operation: ACCEPT_TASK,
        name: "ACCEPT_TASK",
        feature: 1,
        boundary: CallBoundary::Mutation,
        sequence: SequencePolicy::Role,
        delegate_allowed: true,
        payload_min: 72,
        payload_max: 72,
    },
    OperationMetadata {
        operation: CANCEL_TASK,
        name: "CANCEL_TASK",
        feature: 1,
        boundary: CallBoundary::Mutation,
        sequence: SequencePolicy::ObjectLocal,
        delegate_allowed: false,
        payload_min: 32,
        payload_max: 32,
    },
    OperationMetadata {
        operation: COMMIT_TASK_RESULT,
        name: "COMMIT_TASK_RESULT",
        feature: 1,
        boundary: CallBoundary::Mutation,
        sequence: SequencePolicy::Role,
        delegate_allowed: true,
        payload_min: 72,
        payload_max: 72,
    },
    OperationMetadata {
        operation: SEAL_TASK_SET,
        name: "SEAL_TASK_SET",
        feature: 1,
        boundary: CallBoundary::Mutation,
        sequence: SequencePolicy::ObjectLocal,
        delegate_allowed: false,
        payload_min: 48,
        payload_max: 48,
    },
    OperationMetadata {
        operation: OPEN_EPOCH,
        name: "OPEN_EPOCH",
        feature: 1,
        boundary: CallBoundary::Mutation,
        sequence: SequencePolicy::ObjectLocal,
        delegate_allowed: false,
        payload_min: 0,
        payload_max: 15965,
    },
    OperationMetadata {
        operation: EnrollWorker,
        name: "EnrollWorker",
        feature: 2,
        boundary: CallBoundary::Mutation,
        sequence: SequencePolicy::Role,
        delegate_allowed: false,
        payload_min: 136,
        payload_max: 136,
    },
    OperationMetadata {
        operation: PublishMetadata,
        name: "PublishMetadata",
        feature: 2,
        boundary: CallBoundary::Mutation,
        sequence: SequencePolicy::Role,
        delegate_allowed: true,
        payload_min: 0,
        payload_max: 15965,
    },
    OperationMetadata {
        operation: SetDraining,
        name: "SetDraining",
        feature: 2,
        boundary: CallBoundary::Mutation,
        sequence: SequencePolicy::Role,
        delegate_allowed: false,
        payload_min: 0,
        payload_max: 15965,
    },
    OperationMetadata {
        operation: UndoDrain,
        name: "UndoDrain",
        feature: 2,
        boundary: CallBoundary::Mutation,
        sequence: SequencePolicy::Role,
        delegate_allowed: false,
        payload_min: 0,
        payload_max: 15965,
    },
    OperationMetadata {
        operation: RotateDelegate,
        name: "RotateDelegate",
        feature: 2,
        boundary: CallBoundary::Mutation,
        sequence: SequencePolicy::Role,
        delegate_allowed: false,
        payload_min: 0,
        payload_max: 15965,
    },
    OperationMetadata {
        operation: RevokeDelegate,
        name: "RevokeDelegate",
        feature: 2,
        boundary: CallBoundary::Mutation,
        sequence: SequencePolicy::Role,
        delegate_allowed: false,
        payload_min: 0,
        payload_max: 15965,
    },
    OperationMetadata {
        operation: RetireWorker,
        name: "RetireWorker",
        feature: 2,
        boundary: CallBoundary::Mutation,
        sequence: SequencePolicy::Role,
        delegate_allowed: false,
        payload_min: 0,
        payload_max: 15965,
    },
    OperationMetadata {
        operation: AcceptEnrollment,
        name: "AcceptEnrollment",
        feature: 2,
        boundary: CallBoundary::Mutation,
        sequence: SequencePolicy::Role,
        delegate_allowed: false,
        payload_min: 144,
        payload_max: 144,
    },
    OperationMetadata {
        operation: ExpireEnrollment,
        name: "ExpireEnrollment",
        feature: 2,
        boundary: CallBoundary::Mutation,
        sequence: SequencePolicy::Role,
        delegate_allowed: false,
        payload_min: 72,
        payload_max: 72,
    },
    OperationMetadata {
        operation: ScheduleEvaluator,
        name: "ScheduleEvaluator",
        feature: 3,
        boundary: CallBoundary::Mutation,
        sequence: SequencePolicy::Role,
        delegate_allowed: false,
        payload_min: 160,
        payload_max: 160,
    },
    OperationMetadata {
        operation: RotateEvaluatorKey,
        name: "RotateEvaluatorKey",
        feature: 3,
        boundary: CallBoundary::Mutation,
        sequence: SequencePolicy::Role,
        delegate_allowed: false,
        payload_min: 88,
        payload_max: 88,
    },
    OperationMetadata {
        operation: RevokeEvaluator,
        name: "RevokeEvaluator",
        feature: 3,
        boundary: CallBoundary::Mutation,
        sequence: SequencePolicy::Role,
        delegate_allowed: false,
        payload_min: 74,
        payload_max: 74,
    },
    OperationMetadata {
        operation: ChallengeAssessment,
        name: "ChallengeAssessment",
        feature: 3,
        boundary: CallBoundary::Mutation,
        sequence: SequencePolicy::ObjectLocal,
        delegate_allowed: false,
        payload_min: 97,
        payload_max: 97,
    },
    OperationMetadata {
        operation: CommitScore,
        name: "CommitScore",
        feature: 4,
        boundary: CallBoundary::Mutation,
        sequence: SequencePolicy::Role,
        delegate_allowed: true,
        payload_min: 224,
        payload_max: 224,
    },
    OperationMetadata {
        operation: RevealScore,
        name: "RevealScore",
        feature: 4,
        boundary: CallBoundary::Mutation,
        sequence: SequencePolicy::Role,
        delegate_allowed: true,
        payload_min: 364,
        payload_max: 1480,
    },
    OperationMetadata {
        operation: BeginAggregation,
        name: "BeginAggregation",
        feature: 5,
        boundary: CallBoundary::Mutation,
        sequence: SequencePolicy::ObjectLocal,
        delegate_allowed: false,
        payload_min: 0,
        payload_max: 0,
    },
    OperationMetadata {
        operation: ProcessAggregation,
        name: "ProcessAggregation",
        feature: 5,
        boundary: CallBoundary::Mutation,
        sequence: SequencePolicy::ObjectLocal,
        delegate_allowed: false,
        payload_min: 34,
        payload_max: 34,
    },
    OperationMetadata {
        operation: FinalizeAggregation,
        name: "FinalizeAggregation",
        feature: 5,
        boundary: CallBoundary::Mutation,
        sequence: SequencePolicy::ObjectLocal,
        delegate_allowed: false,
        payload_min: 32,
        payload_max: 32,
    },
    OperationMetadata {
        operation: FUND,
        name: "FUND",
        feature: 6,
        boundary: CallBoundary::Mutation,
        sequence: SequencePolicy::Role,
        delegate_allowed: false,
        payload_min: 57,
        payload_max: 57,
    },
    OperationMetadata {
        operation: CLAIM,
        name: "CLAIM",
        feature: 6,
        boundary: CallBoundary::Mutation,
        sequence: SequencePolicy::ObjectLocal,
        delegate_allowed: false,
        payload_min: 80,
        payload_max: 80,
    },
    OperationMetadata {
        operation: EXPIRE_EPOCH_CLAIMS,
        name: "EXPIRE_EPOCH_CLAIMS",
        feature: 6,
        boundary: CallBoundary::Mutation,
        sequence: SequencePolicy::ObjectLocal,
        delegate_allowed: false,
        payload_min: 0,
        payload_max: 0,
    },
    OperationMetadata {
        operation: REFUND_FREE,
        name: "REFUND_FREE",
        feature: 6,
        boundary: CallBoundary::Mutation,
        sequence: SequencePolicy::ObjectLocal,
        delegate_allowed: false,
        payload_min: 64,
        payload_max: 64,
    },
    OperationMetadata {
        operation: PRUNE_EPOCH,
        name: "PRUNE_EPOCH",
        feature: 6,
        boundary: CallBoundary::Mutation,
        sequence: SequencePolicy::ObjectLocal,
        delegate_allowed: false,
        payload_min: 0,
        payload_max: 0,
    },
    OperationMetadata {
        operation: ResetHistory,
        name: "ResetHistory",
        feature: 7,
        boundary: CallBoundary::Mutation,
        sequence: SequencePolicy::Role,
        delegate_allowed: false,
        payload_min: 73,
        payload_max: 73,
    },
    OperationMetadata {
        operation: SuspendHistory,
        name: "SuspendHistory",
        feature: 7,
        boundary: CallBoundary::Mutation,
        sequence: SequencePolicy::Role,
        delegate_allowed: false,
        payload_min: 73,
        payload_max: 73,
    },
    OperationMetadata {
        operation: ResumeHistory,
        name: "ResumeHistory",
        feature: 7,
        boundary: CallBoundary::Mutation,
        sequence: SequencePolicy::Role,
        delegate_allowed: false,
        payload_min: 73,
        payload_max: 73,
    },
    OperationMetadata {
        operation: ApproveAdmission,
        name: "ApproveAdmission",
        feature: 8,
        boundary: CallBoundary::Mutation,
        sequence: SequencePolicy::Role,
        delegate_allowed: false,
        payload_min: 0,
        payload_max: 15965,
    },
    OperationMetadata {
        operation: RevokeAdmissionApproval,
        name: "RevokeAdmissionApproval",
        feature: 8,
        boundary: CallBoundary::Mutation,
        sequence: SequencePolicy::Role,
        delegate_allowed: false,
        payload_min: 0,
        payload_max: 15965,
    },
    OperationMetadata {
        operation: AdmitWorker,
        name: "AdmitWorker",
        feature: 8,
        boundary: CallBoundary::Mutation,
        sequence: SequencePolicy::Role,
        delegate_allowed: false,
        payload_min: 0,
        payload_max: 15965,
    },
    OperationMetadata {
        operation: AdmitEvaluator,
        name: "AdmitEvaluator",
        feature: 8,
        boundary: CallBoundary::Mutation,
        sequence: SequencePolicy::Role,
        delegate_allowed: false,
        payload_min: 0,
        payload_max: 15965,
    },
    OperationMetadata {
        operation: Heartbeat,
        name: "Heartbeat",
        feature: 8,
        boundary: CallBoundary::Mutation,
        sequence: SequencePolicy::Role,
        delegate_allowed: true,
        payload_min: 0,
        payload_max: 15965,
    },
    OperationMetadata {
        operation: RequestExit,
        name: "RequestExit",
        feature: 8,
        boundary: CallBoundary::Mutation,
        sequence: SequencePolicy::Role,
        delegate_allowed: false,
        payload_min: 0,
        payload_max: 15965,
    },
    OperationMetadata {
        operation: CancelExit,
        name: "CancelExit",
        feature: 8,
        boundary: CallBoundary::Mutation,
        sequence: SequencePolicy::Role,
        delegate_allowed: false,
        payload_min: 0,
        payload_max: 15965,
    },
    OperationMetadata {
        operation: PruneInactive,
        name: "PruneInactive",
        feature: 8,
        boundary: CallBoundary::Mutation,
        sequence: SequencePolicy::ObjectLocal,
        delegate_allowed: false,
        payload_min: 0,
        payload_max: 15965,
    },
    OperationMetadata {
        operation: AdministrativeRemove,
        name: "AdministrativeRemove",
        feature: 8,
        boundary: CallBoundary::Mutation,
        sequence: SequencePolicy::Role,
        delegate_allowed: false,
        payload_min: 0,
        payload_max: 15965,
    },
    OperationMetadata {
        operation: SealEvidence,
        name: "SealEvidence",
        feature: 9,
        boundary: CallBoundary::Mutation,
        sequence: SequencePolicy::Role,
        delegate_allowed: true,
        payload_min: 131,
        payload_max: 131,
    },
    OperationMetadata {
        operation: READ_HEADER,
        name: "READ_HEADER",
        feature: 10,
        boundary: CallBoundary::ProgramRead,
        sequence: SequencePolicy::ObjectLocal,
        delegate_allowed: false,
        payload_min: 0,
        payload_max: 0,
    },
    OperationMetadata {
        operation: READ_STATE_CHUNK,
        name: "READ_STATE_CHUNK",
        feature: 10,
        boundary: CallBoundary::ProgramRead,
        sequence: SequencePolicy::ObjectLocal,
        delegate_allowed: false,
        payload_min: 46,
        payload_max: 46,
    },
];

const CONTROL_CAP: usize = Section::Control.payload_cap();
const REBIND_SCRATCH_BYTES: usize = F01_SECTION_CAP + CONTROL_CAP + MAX_STATE_BYTES;
const RECORD_PAYLOAD_BYTES: usize = 192;

const fn max_of(values: &[usize]) -> usize {
    let mut maximum = 0;
    let mut i = 0;
    while i < values.len() {
        if values[i] > maximum {
            maximum = values[i];
        }
        i += 1;
    }
    maximum
}

/// Router scratch: at least the largest scratch any routed transition or the F01 revision
/// rebinding needs.
pub const SCRATCH_BYTES: usize = max_of(&[
    epoch::ADVANCE_SCRATCH_BYTES,
    epoch::OPEN_SCRATCH_BYTES,
    tasks::SCRATCH_BYTES,
    commit_reveal::SCRATCH_BYTES,
    evidence::SEAL_SCRATCH_BYTES,
    challenge::CHALLENGE_SCRATCH_BYTES,
    authority::SCRATCH_BYTES,
    workers::CONTROL_SCRATCH_BYTES,
    F01_SECTION_CAP + CONTROL_CAP,
    REBIND_SCRATCH_BYTES,
]);

/// Caller-owned outputs of one routed call.
pub struct Buffers<'b> {
    pub next: &'b mut [u8],
    pub scratch: &'b mut [u8],
    pub event: &'b mut [u8],
    pub result: &'b mut [u8],
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum Routed {
    /// `next[..state_len]` and `event[..event_len]` must be committed together with the
    /// `Ok` result frame in `result[..result_len]`.
    Applied {
        operation: Operation,
        revision: u64,
        state_len: usize,
        event_len: usize,
        result_len: usize,
    },
    /// Nothing changes; `result[..result_len]` is an `AlreadyApplied` frame.
    Unchanged { result_len: usize },
    /// Nothing changes; `result[..result_len]` is the error frame.
    Refused { result_len: usize },
}

enum Payload {
    Empty,
    Suffix,
    Record([u8; RECORD_PAYLOAD_BYTES], usize),
}
impl Payload {
    fn record(bytes: &[u8]) -> CodecResult<Self> {
        let mut out = [0; RECORD_PAYLOAD_BYTES];
        out.get_mut(..bytes.len())
            .ok_or(NON_CANONICAL)?
            .copy_from_slice(bytes);
        Ok(Self::Record(out, bytes.len()))
    }
}

enum Step {
    Applied {
        revision: u64,
        state_len: usize,
        event_len: usize,
        payload: Payload,
    },
    Retained(RetainedResult),
    Unchanged(Payload),
}

/// Routes one direct native call to its landed feature transition and frames its result.
///
/// Every selector without a landed transition refuses `UNKNOWN_OPERATION`. `current` is the
/// committed `paxai/state/v1` value, absent before CREATE. A refused call writes nothing.
///
/// # Errors
/// Returns an error only when the result frame itself cannot be encoded into `result`.
pub fn route(
    ctx: &CallContext,
    envelope_bytes: &[u8],
    current: Option<&[u8]>,
    buffers: Buffers<'_>,
) -> CodecResult<Routed> {
    let visible = current
        .and_then(|bytes| state::decode_shared_state(bytes).ok())
        .map_or(0, |s| s.revision);
    let envelope = match codec::decode_envelope(envelope_bytes) {
        Ok(envelope) => envelope,
        Err(error) => return refuse(error, Presence::Absent, 0, buffers.result),
    };
    let request = match envelope.request_digest() {
        Ok(request) => request,
        Err(error) => return refuse(error, Presence::Absent, 0, buffers.result),
    };
    let operation = envelope.envelope.operation;
    let Buffers {
        next,
        scratch,
        event,
        result,
    } = buffers;
    let step = transition(
        ctx,
        &envelope,
        envelope_bytes,
        current,
        next,
        scratch,
        event,
    );
    let framed = match step {
        Ok(Step::Applied {
            revision,
            state_len,
            event_len,
            payload,
        }) => applied_frame(
            operation, request, revision, event, event_len, &payload, result,
        )
        .map(|result_len| Routed::Applied {
            operation,
            revision,
            state_len,
            event_len,
            result_len,
        }),
        Ok(Step::Retained(retained)) => {
            let digest = retained.result_digest.bytes();
            frame(
                ApplicationResult::success(
                    ResultStatus::AlreadyApplied,
                    request,
                    retained.applied_revision,
                    &digest,
                ),
                result,
            )
            .map(|result_len| Routed::Unchanged { result_len })
        }
        Ok(Step::Unchanged(payload)) => {
            let bytes: &[u8] = match &payload {
                Payload::Empty | Payload::Suffix => &[],
                Payload::Record(bytes, n) => &bytes[..*n],
            };
            frame(
                ApplicationResult::success(ResultStatus::AlreadyApplied, request, visible, bytes),
                result,
            )
            .map(|result_len| Routed::Unchanged { result_len })
        }
        Err(error) => Err(error),
    };
    match framed {
        Ok(routed) => Ok(routed),
        Err(error) => refuse(error, Presence::Present(request), visible, result),
    }
}

fn frame(value: CodecResult<ApplicationResult<'_>>, out: &mut [u8]) -> CodecResult<usize> {
    codec::encode_result(&value?, out)
}

fn refuse(
    error: crate::errors::ApplicationError,
    request: Presence<RequestDigest>,
    visible: u64,
    out: &mut [u8],
) -> CodecResult<Routed> {
    let result_len =
        codec::encode_result(&ApplicationResult::failure(error, request, visible)?, out)?;
    Ok(Routed::Refused { result_len })
}

fn applied_frame(
    operation: Operation,
    request: RequestDigest,
    revision: u64,
    event: &[u8],
    event_len: usize,
    payload: &Payload,
    out: &mut [u8],
) -> CodecResult<usize> {
    let mut topic = [0; 64];
    let topic_len = codec::event_topic(operation, &mut topic)?;
    let body = event.get(..event_len).ok_or(NON_CANONICAL)?;
    let (named, common, suffix) = codec::decode_event_frame(&topic[..topic_len], body)?;
    let bytes = match payload {
        Payload::Empty => &[],
        Payload::Suffix => suffix,
        Payload::Record(bytes, n) => &bytes[..*n],
    };
    let value = ApplicationResult::success(ResultStatus::Ok, request, revision, bytes)?;
    if named != operation
        || common.revision != revision
        || common.request != request
        || common.result != value.digest
    {
        return Err(NON_CANONICAL);
    }
    codec::encode_result(&value, out)
}

fn transition(
    ctx: &CallContext,
    envelope: &ValidatedEnvelope<'_>,
    envelope_bytes: &[u8],
    current: Option<&[u8]>,
    next: &mut [u8],
    scratch: &mut [u8],
    event: &mut [u8],
) -> CodecResult<Step> {
    let operation = envelope.envelope.operation;
    match operation.0 {
        0x0101..=0x0104 | 0x0106..=0x0109 => registry(ctx, envelope, current, next, scratch, event),
        0x0105 => {
            let a =
                epoch::advance_activation(ctx, envelope, present(current)?, next, scratch, event)?;
            Ok(Step::Applied {
                revision: a.revision,
                state_len: a.state_len,
                event_len: a.event_len,
                payload: Payload::Suffix,
            })
        }
        0x0111 => {
            match epoch::open_epoch(ctx, envelope, present(current)?, next, scratch, event)? {
                epoch::Outcome::Opened {
                    revision,
                    state_len,
                    event_len,
                    ..
                } => Ok(Step::Applied {
                    revision,
                    state_len,
                    event_len,
                    payload: Payload::Suffix,
                }),
                epoch::Outcome::AlreadyApplied { .. } => Ok(Step::Unchanged(Payload::Empty)),
            }
        }
        0x010C..=0x0110 => {
            match tasks::apply(ctx, envelope, present(current)?, next, scratch, event)? {
                tasks::Outcome::Applied {
                    receipt,
                    revision,
                    state_len,
                    event_len,
                } => Ok(Step::Applied {
                    revision,
                    state_len,
                    event_len,
                    payload: Payload::record(receipt.bytes())?,
                }),
                tasks::Outcome::AlreadyApplied { subject } => {
                    Ok(Step::Unchanged(Payload::record(subject.as_bytes())?))
                }
                tasks::Outcome::Retained(retained) => Ok(Step::Retained(retained)),
            }
        }
        0x0201..=0x0207 | 0x0209 | 0x020A | 0x0301..=0x0303 => identity(
            ctx,
            operation,
            envelope_bytes,
            present(current)?,
            next,
            scratch,
            event,
        ),
        0x0304 | 0x0401 | 0x0402 | 0x0901 => {
            assessment(ctx, envelope, present(current)?, next, scratch, event)
        }
        _ => Err(UNKNOWN_OPERATION),
    }
}

fn identity(
    ctx: &CallContext,
    operation: Operation,
    envelope_bytes: &[u8],
    bytes: &[u8],
    next: &mut [u8],
    scratch: &mut [u8],
    event: &mut [u8],
) -> CodecResult<Step> {
    let state = state::decode_shared_state(bytes)?;
    let section = bound_section(ctx, &state)?;
    match operation.0 {
        0x0201..=0x0207 | 0x0209 | 0x020A => {
            let call = workers::CallContext {
                market: &section.header,
                invoking_principal: ctx.principal,
                height: ctx.height,
            };
            match workers::apply(bytes, &call, envelope_bytes, next, event, scratch)? {
                workers::Applied::Applied {
                    state_len,
                    event_len,
                } => rebind(next, state_len, event_len, scratch),
                workers::Applied::AlreadyApplied(retained) => Ok(Step::Retained(retained)),
            }
        }
        0x0301..=0x0303 => {
            let call = authority::AuthorityContext {
                market: &section.header,
                invoking_principal: ctx.principal,
                immediate_caller: Presence::Absent,
                height: ctx.height,
                approved_rubric: section.current.commitments.rubric,
                aggregate_sealed: false,
            };
            match authority::apply(bytes, &call, envelope_bytes, scratch, next, event)? {
                authority::Outcome::Applied {
                    state_len,
                    event_len,
                } => rebind(next, state_len, event_len, scratch),
                authority::Outcome::AlreadyApplied(retained) => Ok(Step::Retained(retained)),
                authority::Outcome::Idempotent => Ok(Step::Unchanged(Payload::Empty)),
            }
        }
        _ => Err(UNKNOWN_OPERATION),
    }
}

fn assessment(
    ctx: &CallContext,
    envelope: &ValidatedEnvelope<'_>,
    current: &[u8],
    next: &mut [u8],
    scratch: &mut [u8],
    event: &mut [u8],
) -> CodecResult<Step> {
    match envelope.envelope.operation.0 {
        0x0304 => match challenge::apply_challenge(ctx, envelope, current, next, scratch, event)? {
            challenge::ChallengeOutcome::Applied {
                record,
                revision,
                state_len,
                event_len,
                ..
            } => Ok(Step::Applied {
                revision,
                state_len,
                event_len,
                payload: Payload::record(&record.encode()?)?,
            }),
            challenge::ChallengeOutcome::AlreadyApplied { record } => {
                Ok(Step::Unchanged(Payload::record(&record.encode()?)?))
            }
        },
        0x0401 | 0x0402 => {
            match commit_reveal::apply(ctx, envelope, current, next, scratch, event)? {
                commit_reveal::Outcome::Committed {
                    record,
                    revision,
                    state_len,
                    event_len,
                    ..
                } => Ok(Step::Applied {
                    revision,
                    state_len,
                    event_len,
                    payload: Payload::record(&record.payload()?)?,
                }),
                commit_reveal::Outcome::Revealed {
                    receipt,
                    revision,
                    state_len,
                    event_len,
                } => Ok(Step::Applied {
                    revision,
                    state_len,
                    event_len,
                    payload: Payload::record(&receipt.payload()?)?,
                }),
                commit_reveal::Outcome::Retained(retained) => Ok(Step::Retained(retained)),
            }
        }
        0x0901 => match evidence::apply(ctx, envelope, current, next, scratch, event)? {
            evidence::Outcome::Applied {
                seal,
                revision,
                state_len,
                event_len,
                ..
            } => Ok(Step::Applied {
                revision,
                state_len,
                event_len,
                payload: Payload::record(&seal.encode()?)?,
            }),
            evidence::Outcome::AlreadyApplied { seal } => {
                Ok(Step::Unchanged(Payload::record(&seal.encode()?)?))
            }
            evidence::Outcome::Retained(retained) => Ok(Step::Retained(retained)),
        },
        _ => Err(UNKNOWN_OPERATION),
    }
}

fn present(current: Option<&[u8]>) -> CodecResult<&[u8]> {
    current.ok_or(NOT_FOUND)
}

fn registry(
    ctx: &CallContext,
    envelope: &ValidatedEnvelope<'_>,
    current: Option<&[u8]>,
    next: &mut [u8],
    scratch: &mut [u8],
    event: &mut [u8],
) -> CodecResult<Step> {
    let decoded = match current {
        Some(bytes) => Some(state::decode_shared_state(bytes)?),
        None => None,
    };
    let (section_out, control) = scratch
        .get_mut(..F01_SECTION_CAP + CONTROL_CAP)
        .ok_or(NON_CANONICAL)?
        .split_at_mut(F01_SECTION_CAP);
    match registry_ops::apply(ctx, decoded.as_ref(), envelope, section_out, event)? {
        registry_ops::Outcome::Applied {
            state,
            receipt,
            event_len,
        } => Ok(Step::Applied {
            revision: state.revision,
            state_len: state::encode_shared_state(&state, next, control)?,
            event_len,
            payload: Payload::record(receipt.bytes())?,
        }),
        registry_ops::Outcome::AlreadyApplied(retained) => Ok(Step::Retained(retained)),
    }
}

fn bound_section<'a>(ctx: &CallContext, state: &SharedState<'a>) -> CodecResult<PolicySection<'a>> {
    let section = PolicySection::decode(state.feature_sections[Section::PolicyLifecycle.index()])?;
    if section.header.state_revision != state.revision {
        return Err(NON_CANONICAL);
    }
    if section.header.deployment_chain_domain != ctx.chain {
        return Err(WRONG_DOMAIN);
    }
    if section.header.program_id != ctx.program {
        return Err(WRONG_PROGRAM);
    }
    Ok(section)
}

fn rebind(
    next: &mut [u8],
    state_len: usize,
    event_len: usize,
    scratch: &mut [u8],
) -> CodecResult<Step> {
    let (section_out, rest) = scratch
        .get_mut(..REBIND_SCRATCH_BYTES)
        .ok_or(NON_CANONICAL)?
        .split_at_mut(F01_SECTION_CAP);
    let (control, staged) = rest.split_at_mut(CONTROL_CAP);
    let (revision, staged_len) = {
        let state = state::decode_shared_state(next.get(..state_len).ok_or(NON_CANONICAL)?)?;
        let mut section =
            PolicySection::decode(state.feature_sections[Section::PolicyLifecycle.index()])?;
        section.header.state_revision = state.revision;
        let section_len = section.encode(section_out)?;
        let rebound =
            state.replace_section(Section::PolicyLifecycle, &section_out[..section_len])?;
        (
            state.revision,
            state::encode_shared_state(&rebound, staged, control)?,
        )
    };
    next.get_mut(..staged_len)
        .ok_or(NON_CANONICAL)?
        .copy_from_slice(&staged[..staged_len]);
    Ok(Step::Applied {
        revision,
        state_len: staged_len,
        event_len,
        payload: Payload::Suffix,
    })
}
