#![allow(non_upper_case_globals)]
//! Fixed selector routing. Feature owners supply transitions; no handler executes here.
use crate::errors::{CodecResult, NON_CANONICAL, UNKNOWN_OPERATION};
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
    pub fn decode(selector: u16) -> CodecResult<Self> {
        OPERATIONS
            .iter()
            .find(|m| m.operation.0 == selector)
            .map(|m| m.operation)
            .ok_or(UNKNOWN_OPERATION)
    }
    pub const fn selector(self) -> u16 {
        self.0
    }
    pub fn metadata(self) -> &'static OperationMetadata {
        // The private constructor and static table make this branch unreachable.
        for m in OPERATIONS {
            if m.operation == self {
                return m;
            }
        }
        unreachable!("Operation is constructed only by the frozen table")
    }
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
