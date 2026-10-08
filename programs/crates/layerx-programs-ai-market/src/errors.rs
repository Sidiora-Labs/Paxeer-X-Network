//! Frozen application and independent offchain error spaces. Native errors are untouched.
use core::fmt;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ApplicationError(u16);
pub type CodecResult<T> = Result<T, ApplicationError>;
impl ApplicationError {
    pub fn from_code(code: u16) -> CodecResult<Self> {
        if APPLICATION_ERRORS.iter().any(|e| e.code == code) {
            Ok(Self(code))
        } else {
            Err(NON_CANONICAL)
        }
    }
    pub const fn code(self) -> u16 {
        self.0
    }
    pub fn named(feature: &str, name: &str) -> CodecResult<Self> {
        APPLICATION_ERRORS
            .iter()
            .chain(APPLICATION_ALIASES.iter())
            .find(|e| e.feature == feature && e.name == name)
            .map(|e| Self(e.code))
            .ok_or(NON_CANONICAL)
    }
}
impl fmt::Display for ApplicationError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "PAXAI application error {:04x}", self.0)
    }
}
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ErrorEntry {
    pub feature: &'static str,
    pub name: &'static str,
    pub code: u16,
}
pub const BAD_VERSION: ApplicationError = ApplicationError(0x0001);
pub const NON_CANONICAL: ApplicationError = ApplicationError(0x0002);
pub const WRONG_DOMAIN: ApplicationError = ApplicationError(0x0003);
pub const WRONG_PROGRAM: ApplicationError = ApplicationError(0x0004);
pub const WRONG_MARKET: ApplicationError = ApplicationError(0x0005);
pub const UNAUTHORIZED: ApplicationError = ApplicationError(0x0006);
pub const ROLE_CONFLICT: ApplicationError = ApplicationError(0x0007);
pub const REVOKED: ApplicationError = ApplicationError(0x0008);
pub const KEY_MISMATCH: ApplicationError = ApplicationError(0x0009);
pub const BAD_SIGNATURE: ApplicationError = ApplicationError(0x000a);
pub const EXPIRED: ApplicationError = ApplicationError(0x000b);
pub const WRONG_EPOCH: ApplicationError = ApplicationError(0x000c);
pub const WRONG_CONFIG: ApplicationError = ApplicationError(0x000d);
pub const WRONG_ROSTER: ApplicationError = ApplicationError(0x000e);
pub const WRONG_PHASE: ApplicationError = ApplicationError(0x000f);
pub const REPLAY_CONFLICT: ApplicationError = ApplicationError(0x0010);
pub const SEQUENCE_CONSUMED: ApplicationError = ApplicationError(0x0011);
pub const SEQUENCE_GAP: ApplicationError = ApplicationError(0x0012);
pub const STALE_CURSOR: ApplicationError = ApplicationError(0x0013);
pub const CONFLICT: ApplicationError = ApplicationError(0x0014);
pub const NOT_FOUND: ApplicationError = ApplicationError(0x0015);
pub const CAPACITY: ApplicationError = ApplicationError(0x0016);
pub const RETENTION_FULL: ApplicationError = ApplicationError(0x0017);
pub const INSUFFICIENT_FREE: ApplicationError = ApplicationError(0x0018);
pub const ACCOUNT_BINDING: ApplicationError = ApplicationError(0x0019);
pub const HOST_CAPABILITY: ApplicationError = ApplicationError(0x001a);
pub const HOST_TRANSFER: ApplicationError = ApplicationError(0x001b);
pub const ARITHMETIC: ApplicationError = ApplicationError(0x001c);
pub const EVIDENCE_BINDING: ApplicationError = ApplicationError(0x001d);
pub const READINESS_BLOCKED: ApplicationError = ApplicationError(0x001e);
pub const UNKNOWN_OPERATION: ApplicationError = ApplicationError(0x001f);
pub const F03_NO_GRANT: ApplicationError = ApplicationError(0x0301);
pub const F03_EVALUATOR_CAPACITY: ApplicationError = ApplicationError(0x0302);
pub const F03_BAD_ACTIVATION: ApplicationError = ApplicationError(0x0303);
pub const F03_KEY_REUSED: ApplicationError = ApplicationError(0x0304);
pub const F03_KEY_VERSION_CONFLICT: ApplicationError = ApplicationError(0x0305);
pub const F03_GRANT_VERSION_CONFLICT: ApplicationError = ApplicationError(0x0306);
pub const F03_REPORT_ALREADY_FINAL: ApplicationError = ApplicationError(0x0307);
pub const F03_CHALLENGE_CAPACITY: ApplicationError = ApplicationError(0x0308);
pub const F03_UNKNOWN_WORKER: ApplicationError = ApplicationError(0x0309);
pub const F03_NO_SCORES: ApplicationError = ApplicationError(0x030a);
pub const F03_SCORE_RANGE: ApplicationError = ApplicationError(0x030b);
pub const F03_NONCANONICAL_VECTOR: ApplicationError = ApplicationError(0x030c);
pub const F03_EVIDENCE_NOT_SEALED: ApplicationError = ApplicationError(0x030d);
pub const F03_EVIDENCE_ROOT_MISMATCH: ApplicationError = ApplicationError(0x030e);
pub const F06_FUNDING_POLICY_MISMATCH: ApplicationError = ApplicationError(0x0601);
pub const F06_REFUND_RECIPIENT_MISMATCH: ApplicationError = ApplicationError(0x0602);
pub const F06_WRONG_ASSET: ApplicationError = ApplicationError(0x0603);
pub const F06_INVALID_AMOUNT: ApplicationError = ApplicationError(0x0604);
pub const F06_EPOCH_ALREADY_RESERVED: ApplicationError = ApplicationError(0x0605);
pub const F06_EPOCH_NOT_RESERVED: ApplicationError = ApplicationError(0x0606);
pub const F06_AGGREGATION_MISMATCH: ApplicationError = ApplicationError(0x0607);
pub const F06_EPOCH_TERMINAL: ApplicationError = ApplicationError(0x0608);
pub const F06_CLAIM_NOT_READY: ApplicationError = ApplicationError(0x0609);
pub const F06_CLAIM_EXPIRED: ApplicationError = ApplicationError(0x060a);
pub const F06_UNKNOWN_WORKER_ENTITLEMENT: ApplicationError = ApplicationError(0x060b);
pub const F06_WRONG_CLAIM_RECIPIENT: ApplicationError = ApplicationError(0x060c);
pub const F06_WRONG_CLAIM_AMOUNT: ApplicationError = ApplicationError(0x060d);
pub const F06_NOTHING_TO_CLAIM: ApplicationError = ApplicationError(0x060e);
pub const F06_LEDGER_INVARIANT_VIOLATION: ApplicationError = ApplicationError(0x060f);
pub const F06_CONTRIBUTION_CONSENT_REQUIRED: ApplicationError = ApplicationError(0x0610);
pub const F01_INVALID_POLICY: ApplicationError = ApplicationError(0x0101);
pub const F01_PRINCIPAL_MISMATCH: ApplicationError = ApplicationError(0x0102);
pub const F01_STALE_REVISION: ApplicationError = ApplicationError(0x0103);
pub const F01_ALREADY_CREATED: ApplicationError = ApplicationError(0x0104);
pub const F01_ACCOUNT_BINDING_MISSING: ApplicationError = ApplicationError(0x0105);
pub const F01_VERSION_MISMATCH: ApplicationError = ApplicationError(0x0106);
pub const F01_PENDING_POLICY_EXISTS: ApplicationError = ApplicationError(0x0107);
pub const F01_NO_PENDING_POLICY: ApplicationError = ApplicationError(0x0108);
pub const F01_ACTIVATION_TOO_EARLY: ApplicationError = ApplicationError(0x0109);
pub const F01_ACTIVATION_NOT_READY: ApplicationError = ApplicationError(0x010a);
pub const F01_ALREADY_ACTIVATED: ApplicationError = ApplicationError(0x010b);
pub const F01_WRONG_LIFECYCLE: ApplicationError = ApplicationError(0x010c);
pub const F01_LIFECYCLE_CLOSED: ApplicationError = ApplicationError(0x010d);
pub const F01_INVALID_REASON: ApplicationError = ApplicationError(0x010e);
pub const F01_GRANT_ALREADY_REVOKED: ApplicationError = ApplicationError(0x010f);
pub const F01_ALREADY_CLOSING: ApplicationError = ApplicationError(0x0110);
pub const F01_OBLIGATIONS_OUTSTANDING: ApplicationError = ApplicationError(0x0111);
pub const F01_CAPACITY_UNAVAILABLE: ApplicationError = ApplicationError(0x0112);
pub const F01_NO_TASK_CAPACITY: ApplicationError = ApplicationError(0x0113);
pub const F01_UNKNOWN_WORKER: ApplicationError = ApplicationError(0x0114);
pub const F01_TASK_CONFLICT: ApplicationError = ApplicationError(0x0115);
pub const F01_TASK_EXPIRED: ApplicationError = ApplicationError(0x0116);
pub const F01_POLICY_MISMATCH: ApplicationError = ApplicationError(0x0117);
pub const F01_TASK_NOT_FOUND: ApplicationError = ApplicationError(0x0118);
pub const F01_WRONG_WORKER: ApplicationError = ApplicationError(0x0119);
pub const F01_TASK_ALREADY_ACCEPTED: ApplicationError = ApplicationError(0x011a);
pub const F02_OWNER_REQUIRED: ApplicationError = ApplicationError(0x0201);
pub const F02_DELEGATE_CONSENT_REQUIRED: ApplicationError = ApplicationError(0x0202);
pub const F02_DELEGATE_REVOKED: ApplicationError = ApplicationError(0x0203);
pub const F02_IDENTITY_FROZEN: ApplicationError = ApplicationError(0x0204);
pub const F02_STALE_AUTHORITY: ApplicationError = ApplicationError(0x0205);
pub const F02_WRONG_GENERATION: ApplicationError = ApplicationError(0x0206);
pub const F02_WRONG_REVISION: ApplicationError = ApplicationError(0x0207);
pub const F02_METADATA_EXPIRED: ApplicationError = ApplicationError(0x0208);
pub const F02_METADATA_UNAVAILABLE: ApplicationError = ApplicationError(0x0209);
pub const F02_METADATA_INTEGRITY_FAILURE: ApplicationError = ApplicationError(0x020a);
pub const F02_CAPABILITY_MISMATCH: ApplicationError = ApplicationError(0x020b);
pub const F02_ADMISSION_NOT_EFFECTIVE: ApplicationError = ApplicationError(0x020c);
pub const F02_MARKET_PAUSED: ApplicationError = ApplicationError(0x020d);
pub const F02_RATE_LIMITED: ApplicationError = ApplicationError(0x020e);
pub const F02_INPUT_TOO_LARGE: ApplicationError = ApplicationError(0x020f);
pub const F02_OUTPUT_TOO_LARGE: ApplicationError = ApplicationError(0x0210);
pub const F02_DEADLINE_INVALID: ApplicationError = ApplicationError(0x0211);
pub const F02_ACCESS_DENIED: ApplicationError = ApplicationError(0x0212);
pub const F02_UNKNOWN_EXECUTION: ApplicationError = ApplicationError(0x0213);
pub const F04_NO_SCORES: ApplicationError = ApplicationError(0x0401);
pub const F04_SALT_INVALID: ApplicationError = ApplicationError(0x0402);
pub const F04_COMMIT_MISMATCH: ApplicationError = ApplicationError(0x0403);
pub const F05_REPORT_INVARIANT: ApplicationError = ApplicationError(0x0501);
pub const F07_IDENTITY_FROZEN: ApplicationError = ApplicationError(0x0701);
pub const F07_SEGMENT_MISMATCH: ApplicationError = ApplicationError(0x0702);
pub const F07_GENERATION_MISMATCH: ApplicationError = ApplicationError(0x0703);
pub const F07_UNKNOWN_WORKER: ApplicationError = ApplicationError(0x0704);
pub const F07_EPOCH_NOT_SEALED: ApplicationError = ApplicationError(0x0705);
pub const F07_BINDING_MISMATCH: ApplicationError = ApplicationError(0x0706);
pub const F07_RESOURCE_LIMIT: ApplicationError = ApplicationError(0x0707);
pub const F07_FINALITY_UNAVAILABLE: ApplicationError = ApplicationError(0x0708);
pub const F08_OWNER_REQUIRED: ApplicationError = ApplicationError(0x0801);
pub const F08_ADMINISTRATOR_APPROVAL_REQUIRED: ApplicationError = ApplicationError(0x0802);
pub const F08_BAD_CONSENT: ApplicationError = ApplicationError(0x0803);
pub const F08_PERMIT_EXPIRED: ApplicationError = ApplicationError(0x0804);
pub const F08_PERMIT_CONSUMED: ApplicationError = ApplicationError(0x0805);
pub const F08_IDENTITY_FROZEN: ApplicationError = ApplicationError(0x0806);
pub const F08_DELEGATE_REVOKED: ApplicationError = ApplicationError(0x0807);
pub const F08_WRONG_GENERATION: ApplicationError = ApplicationError(0x0808);
pub const F08_MARKET_PAUSED: ApplicationError = ApplicationError(0x0809);
pub const F08_DUPLICATE_IDENTITY: ApplicationError = ApplicationError(0x080a);
pub const F08_OWNER_CAPACITY_EXCEEDED: ApplicationError = ApplicationError(0x080b);
pub const F08_CAPACITY_EXCEEDED: ApplicationError = ApplicationError(0x080c);
pub const F08_ADMISSION_WINDOW_FULL: ApplicationError = ApplicationError(0x080d);
pub const F08_RATE_LIMITED: ApplicationError = ApplicationError(0x080e);
pub const F08_NO_PRUNABLE_MEMBER: ApplicationError = ApplicationError(0x080f);
pub const F08_CANDIDATE_CHANGED: ApplicationError = ApplicationError(0x0810);
pub const F08_STALE_STATE: ApplicationError = ApplicationError(0x0811);
pub const F08_RETENTION_BLOCKED: ApplicationError = ApplicationError(0x0812);
pub const F08_QUORUM_UNAVAILABLE: ApplicationError = ApplicationError(0x0813);
pub const F08_IDEMPOTENCY_CONFLICT: ApplicationError = ApplicationError(0x0814);
pub const F08_RESOURCE_EXHAUSTED: ApplicationError = ApplicationError(0x0815);
pub const F09_EVIDENCE_TASK_SET_UNSEALED: ApplicationError = ApplicationError(0x0901);
pub const F09_EVIDENCE_SEAL_CONFLICT: ApplicationError = ApplicationError(0x0902);
pub const F07_IDEMPOTENCY_CONFLICT: ApplicationError = ApplicationError(0x0709);
pub const APPLICATION_ERRORS: &[ErrorEntry] = &[
    ErrorEntry {
        feature: "F07",
        name: "IdempotencyConflict",
        code: 0x0709,
    },
    ErrorEntry {
        feature: "COMMON",
        name: "BAD_VERSION",
        code: 0x0001,
    },
    ErrorEntry {
        feature: "COMMON",
        name: "NON_CANONICAL",
        code: 0x0002,
    },
    ErrorEntry {
        feature: "COMMON",
        name: "WRONG_DOMAIN",
        code: 0x0003,
    },
    ErrorEntry {
        feature: "COMMON",
        name: "WRONG_PROGRAM",
        code: 0x0004,
    },
    ErrorEntry {
        feature: "COMMON",
        name: "WRONG_MARKET",
        code: 0x0005,
    },
    ErrorEntry {
        feature: "COMMON",
        name: "UNAUTHORIZED",
        code: 0x0006,
    },
    ErrorEntry {
        feature: "COMMON",
        name: "ROLE_CONFLICT",
        code: 0x0007,
    },
    ErrorEntry {
        feature: "COMMON",
        name: "REVOKED",
        code: 0x0008,
    },
    ErrorEntry {
        feature: "COMMON",
        name: "KEY_MISMATCH",
        code: 0x0009,
    },
    ErrorEntry {
        feature: "COMMON",
        name: "BAD_SIGNATURE",
        code: 0x000a,
    },
    ErrorEntry {
        feature: "COMMON",
        name: "EXPIRED",
        code: 0x000b,
    },
    ErrorEntry {
        feature: "COMMON",
        name: "WRONG_EPOCH",
        code: 0x000c,
    },
    ErrorEntry {
        feature: "COMMON",
        name: "WRONG_CONFIG",
        code: 0x000d,
    },
    ErrorEntry {
        feature: "COMMON",
        name: "WRONG_ROSTER",
        code: 0x000e,
    },
    ErrorEntry {
        feature: "COMMON",
        name: "WRONG_PHASE",
        code: 0x000f,
    },
    ErrorEntry {
        feature: "COMMON",
        name: "REPLAY_CONFLICT",
        code: 0x0010,
    },
    ErrorEntry {
        feature: "COMMON",
        name: "SEQUENCE_CONSUMED",
        code: 0x0011,
    },
    ErrorEntry {
        feature: "COMMON",
        name: "SEQUENCE_GAP",
        code: 0x0012,
    },
    ErrorEntry {
        feature: "COMMON",
        name: "STALE_CURSOR",
        code: 0x0013,
    },
    ErrorEntry {
        feature: "COMMON",
        name: "CONFLICT",
        code: 0x0014,
    },
    ErrorEntry {
        feature: "COMMON",
        name: "NOT_FOUND",
        code: 0x0015,
    },
    ErrorEntry {
        feature: "COMMON",
        name: "CAPACITY",
        code: 0x0016,
    },
    ErrorEntry {
        feature: "COMMON",
        name: "RETENTION_FULL",
        code: 0x0017,
    },
    ErrorEntry {
        feature: "COMMON",
        name: "INSUFFICIENT_FREE",
        code: 0x0018,
    },
    ErrorEntry {
        feature: "COMMON",
        name: "ACCOUNT_BINDING",
        code: 0x0019,
    },
    ErrorEntry {
        feature: "COMMON",
        name: "HOST_CAPABILITY",
        code: 0x001a,
    },
    ErrorEntry {
        feature: "COMMON",
        name: "HOST_TRANSFER",
        code: 0x001b,
    },
    ErrorEntry {
        feature: "COMMON",
        name: "ARITHMETIC",
        code: 0x001c,
    },
    ErrorEntry {
        feature: "COMMON",
        name: "EVIDENCE_BINDING",
        code: 0x001d,
    },
    ErrorEntry {
        feature: "COMMON",
        name: "READINESS_BLOCKED",
        code: 0x001e,
    },
    ErrorEntry {
        feature: "COMMON",
        name: "UNKNOWN_OPERATION",
        code: 0x001f,
    },
    ErrorEntry {
        feature: "F03",
        name: "NO_GRANT",
        code: 0x0301,
    },
    ErrorEntry {
        feature: "F03",
        name: "EVALUATOR_CAPACITY",
        code: 0x0302,
    },
    ErrorEntry {
        feature: "F03",
        name: "BAD_ACTIVATION",
        code: 0x0303,
    },
    ErrorEntry {
        feature: "F03",
        name: "KEY_REUSED",
        code: 0x0304,
    },
    ErrorEntry {
        feature: "F03",
        name: "KEY_VERSION_CONFLICT",
        code: 0x0305,
    },
    ErrorEntry {
        feature: "F03",
        name: "GRANT_VERSION_CONFLICT",
        code: 0x0306,
    },
    ErrorEntry {
        feature: "F03",
        name: "REPORT_ALREADY_FINAL",
        code: 0x0307,
    },
    ErrorEntry {
        feature: "F03",
        name: "CHALLENGE_CAPACITY",
        code: 0x0308,
    },
    ErrorEntry {
        feature: "F03",
        name: "UNKNOWN_WORKER",
        code: 0x0309,
    },
    ErrorEntry {
        feature: "F03",
        name: "NO_SCORES",
        code: 0x030a,
    },
    ErrorEntry {
        feature: "F03",
        name: "SCORE_RANGE",
        code: 0x030b,
    },
    ErrorEntry {
        feature: "F03",
        name: "NONCANONICAL_VECTOR",
        code: 0x030c,
    },
    ErrorEntry {
        feature: "F03",
        name: "EVIDENCE_NOT_SEALED",
        code: 0x030d,
    },
    ErrorEntry {
        feature: "F03",
        name: "EVIDENCE_ROOT_MISMATCH",
        code: 0x030e,
    },
    ErrorEntry {
        feature: "F06",
        name: "FUNDING_POLICY_MISMATCH",
        code: 0x0601,
    },
    ErrorEntry {
        feature: "F06",
        name: "REFUND_RECIPIENT_MISMATCH",
        code: 0x0602,
    },
    ErrorEntry {
        feature: "F06",
        name: "WRONG_ASSET",
        code: 0x0603,
    },
    ErrorEntry {
        feature: "F06",
        name: "INVALID_AMOUNT",
        code: 0x0604,
    },
    ErrorEntry {
        feature: "F06",
        name: "EPOCH_ALREADY_RESERVED",
        code: 0x0605,
    },
    ErrorEntry {
        feature: "F06",
        name: "EPOCH_NOT_RESERVED",
        code: 0x0606,
    },
    ErrorEntry {
        feature: "F06",
        name: "AGGREGATION_MISMATCH",
        code: 0x0607,
    },
    ErrorEntry {
        feature: "F06",
        name: "EPOCH_TERMINAL",
        code: 0x0608,
    },
    ErrorEntry {
        feature: "F06",
        name: "CLAIM_NOT_READY",
        code: 0x0609,
    },
    ErrorEntry {
        feature: "F06",
        name: "CLAIM_EXPIRED",
        code: 0x060a,
    },
    ErrorEntry {
        feature: "F06",
        name: "UNKNOWN_WORKER_ENTITLEMENT",
        code: 0x060b,
    },
    ErrorEntry {
        feature: "F06",
        name: "WRONG_CLAIM_RECIPIENT",
        code: 0x060c,
    },
    ErrorEntry {
        feature: "F06",
        name: "WRONG_CLAIM_AMOUNT",
        code: 0x060d,
    },
    ErrorEntry {
        feature: "F06",
        name: "NOTHING_TO_CLAIM",
        code: 0x060e,
    },
    ErrorEntry {
        feature: "F06",
        name: "LEDGER_INVARIANT_VIOLATION",
        code: 0x060f,
    },
    ErrorEntry {
        feature: "F06",
        name: "CONTRIBUTION_CONSENT_REQUIRED",
        code: 0x0610,
    },
    ErrorEntry {
        feature: "F01",
        name: "InvalidPolicy",
        code: 0x0101,
    },
    ErrorEntry {
        feature: "F01",
        name: "PrincipalMismatch",
        code: 0x0102,
    },
    ErrorEntry {
        feature: "F01",
        name: "StaleRevision",
        code: 0x0103,
    },
    ErrorEntry {
        feature: "F01",
        name: "AlreadyCreated",
        code: 0x0104,
    },
    ErrorEntry {
        feature: "F01",
        name: "AccountBindingMissing",
        code: 0x0105,
    },
    ErrorEntry {
        feature: "F01",
        name: "VersionMismatch",
        code: 0x0106,
    },
    ErrorEntry {
        feature: "F01",
        name: "PendingPolicyExists",
        code: 0x0107,
    },
    ErrorEntry {
        feature: "F01",
        name: "NoPendingPolicy",
        code: 0x0108,
    },
    ErrorEntry {
        feature: "F01",
        name: "ActivationTooEarly",
        code: 0x0109,
    },
    ErrorEntry {
        feature: "F01",
        name: "ActivationNotReady",
        code: 0x010a,
    },
    ErrorEntry {
        feature: "F01",
        name: "AlreadyActivated",
        code: 0x010b,
    },
    ErrorEntry {
        feature: "F01",
        name: "WrongLifecycle",
        code: 0x010c,
    },
    ErrorEntry {
        feature: "F01",
        name: "LifecycleClosed",
        code: 0x010d,
    },
    ErrorEntry {
        feature: "F01",
        name: "InvalidReason",
        code: 0x010e,
    },
    ErrorEntry {
        feature: "F01",
        name: "GrantAlreadyRevoked",
        code: 0x010f,
    },
    ErrorEntry {
        feature: "F01",
        name: "AlreadyClosing",
        code: 0x0110,
    },
    ErrorEntry {
        feature: "F01",
        name: "ObligationsOutstanding",
        code: 0x0111,
    },
    ErrorEntry {
        feature: "F01",
        name: "CapacityUnavailable",
        code: 0x0112,
    },
    ErrorEntry {
        feature: "F01",
        name: "NoTaskCapacity",
        code: 0x0113,
    },
    ErrorEntry {
        feature: "F01",
        name: "UnknownWorker",
        code: 0x0114,
    },
    ErrorEntry {
        feature: "F01",
        name: "TaskConflict",
        code: 0x0115,
    },
    ErrorEntry {
        feature: "F01",
        name: "TaskExpired",
        code: 0x0116,
    },
    ErrorEntry {
        feature: "F01",
        name: "PolicyMismatch",
        code: 0x0117,
    },
    ErrorEntry {
        feature: "F01",
        name: "TaskNotFound",
        code: 0x0118,
    },
    ErrorEntry {
        feature: "F01",
        name: "WrongWorker",
        code: 0x0119,
    },
    ErrorEntry {
        feature: "F01",
        name: "TaskAlreadyAccepted",
        code: 0x011a,
    },
    ErrorEntry {
        feature: "F02",
        name: "OwnerRequired",
        code: 0x0201,
    },
    ErrorEntry {
        feature: "F02",
        name: "DelegateConsentRequired",
        code: 0x0202,
    },
    ErrorEntry {
        feature: "F02",
        name: "DelegateRevoked",
        code: 0x0203,
    },
    ErrorEntry {
        feature: "F02",
        name: "IdentityFrozen",
        code: 0x0204,
    },
    ErrorEntry {
        feature: "F02",
        name: "StaleAuthority",
        code: 0x0205,
    },
    ErrorEntry {
        feature: "F02",
        name: "WrongGeneration",
        code: 0x0206,
    },
    ErrorEntry {
        feature: "F02",
        name: "WrongRevision",
        code: 0x0207,
    },
    ErrorEntry {
        feature: "F02",
        name: "MetadataExpired",
        code: 0x0208,
    },
    ErrorEntry {
        feature: "F02",
        name: "MetadataUnavailable",
        code: 0x0209,
    },
    ErrorEntry {
        feature: "F02",
        name: "MetadataIntegrityFailure",
        code: 0x020a,
    },
    ErrorEntry {
        feature: "F02",
        name: "CapabilityMismatch",
        code: 0x020b,
    },
    ErrorEntry {
        feature: "F02",
        name: "AdmissionNotEffective",
        code: 0x020c,
    },
    ErrorEntry {
        feature: "F02",
        name: "MarketPaused",
        code: 0x020d,
    },
    ErrorEntry {
        feature: "F02",
        name: "RateLimited",
        code: 0x020e,
    },
    ErrorEntry {
        feature: "F02",
        name: "InputTooLarge",
        code: 0x020f,
    },
    ErrorEntry {
        feature: "F02",
        name: "OutputTooLarge",
        code: 0x0210,
    },
    ErrorEntry {
        feature: "F02",
        name: "DeadlineInvalid",
        code: 0x0211,
    },
    ErrorEntry {
        feature: "F02",
        name: "AccessDenied",
        code: 0x0212,
    },
    ErrorEntry {
        feature: "F02",
        name: "UnknownExecution",
        code: 0x0213,
    },
    ErrorEntry {
        feature: "F04",
        name: "F04_NO_SCORES",
        code: 0x0401,
    },
    ErrorEntry {
        feature: "F04",
        name: "F04_SALT_INVALID",
        code: 0x0402,
    },
    ErrorEntry {
        feature: "F04",
        name: "F04_COMMIT_MISMATCH",
        code: 0x0403,
    },
    ErrorEntry {
        feature: "F05",
        name: "F05_REPORT_INVARIANT",
        code: 0x0501,
    },
    ErrorEntry {
        feature: "F07",
        name: "IdentityFrozen",
        code: 0x0701,
    },
    ErrorEntry {
        feature: "F07",
        name: "SegmentMismatch",
        code: 0x0702,
    },
    ErrorEntry {
        feature: "F07",
        name: "GenerationMismatch",
        code: 0x0703,
    },
    ErrorEntry {
        feature: "F07",
        name: "UnknownWorker",
        code: 0x0704,
    },
    ErrorEntry {
        feature: "F07",
        name: "EpochNotSealed",
        code: 0x0705,
    },
    ErrorEntry {
        feature: "F07",
        name: "BindingMismatch",
        code: 0x0706,
    },
    ErrorEntry {
        feature: "F07",
        name: "ResourceLimit",
        code: 0x0707,
    },
    ErrorEntry {
        feature: "F07",
        name: "FinalityUnavailable",
        code: 0x0708,
    },
    ErrorEntry {
        feature: "F08",
        name: "OwnerRequired",
        code: 0x0801,
    },
    ErrorEntry {
        feature: "F08",
        name: "AdministratorApprovalRequired",
        code: 0x0802,
    },
    ErrorEntry {
        feature: "F08",
        name: "BadConsent",
        code: 0x0803,
    },
    ErrorEntry {
        feature: "F08",
        name: "PermitExpired",
        code: 0x0804,
    },
    ErrorEntry {
        feature: "F08",
        name: "PermitConsumed",
        code: 0x0805,
    },
    ErrorEntry {
        feature: "F08",
        name: "IdentityFrozen",
        code: 0x0806,
    },
    ErrorEntry {
        feature: "F08",
        name: "DelegateRevoked",
        code: 0x0807,
    },
    ErrorEntry {
        feature: "F08",
        name: "WrongGeneration",
        code: 0x0808,
    },
    ErrorEntry {
        feature: "F08",
        name: "MarketPaused",
        code: 0x0809,
    },
    ErrorEntry {
        feature: "F08",
        name: "DuplicateIdentity",
        code: 0x080a,
    },
    ErrorEntry {
        feature: "F08",
        name: "OwnerCapacityExceeded",
        code: 0x080b,
    },
    ErrorEntry {
        feature: "F08",
        name: "CapacityExceeded",
        code: 0x080c,
    },
    ErrorEntry {
        feature: "F08",
        name: "AdmissionWindowFull",
        code: 0x080d,
    },
    ErrorEntry {
        feature: "F08",
        name: "RateLimited",
        code: 0x080e,
    },
    ErrorEntry {
        feature: "F08",
        name: "NoPrunableMember",
        code: 0x080f,
    },
    ErrorEntry {
        feature: "F08",
        name: "CandidateChanged",
        code: 0x0810,
    },
    ErrorEntry {
        feature: "F08",
        name: "StaleState",
        code: 0x0811,
    },
    ErrorEntry {
        feature: "F08",
        name: "RetentionBlocked",
        code: 0x0812,
    },
    ErrorEntry {
        feature: "F08",
        name: "QuorumUnavailable",
        code: 0x0813,
    },
    ErrorEntry {
        feature: "F08",
        name: "IdempotencyConflict",
        code: 0x0814,
    },
    ErrorEntry {
        feature: "F08",
        name: "ResourceExhausted",
        code: 0x0815,
    },
    ErrorEntry {
        feature: "F09",
        name: "EVIDENCE_TASK_SET_UNSEALED",
        code: 0x0901,
    },
    ErrorEntry {
        feature: "F09",
        name: "EVIDENCE_SEAL_CONFLICT",
        code: 0x0902,
    },
];
pub const APPLICATION_ALIASES: &[ErrorEntry] = &[
    ErrorEntry {
        feature: "F01",
        name: "InvalidEncoding",
        code: 0x0002,
    },
    ErrorEntry {
        feature: "F01",
        name: "WrongDomain",
        code: 0x0003,
    },
    ErrorEntry {
        feature: "F01",
        name: "Unauthorized",
        code: 0x0006,
    },
    ErrorEntry {
        feature: "F01",
        name: "ArithmeticOverflow",
        code: 0x001c,
    },
    ErrorEntry {
        feature: "F02",
        name: "NonCanonical",
        code: 0x0002,
    },
    ErrorEntry {
        feature: "F02",
        name: "UnsupportedVersion",
        code: 0x0001,
    },
    ErrorEntry {
        feature: "F02",
        name: "WrongDomain",
        code: 0x0003,
    },
    ErrorEntry {
        feature: "F02",
        name: "BadSignature",
        code: 0x000a,
    },
    ErrorEntry {
        feature: "F02",
        name: "CapacityExceeded",
        code: 0x0016,
    },
    ErrorEntry {
        feature: "F02",
        name: "IdempotencyConflict",
        code: 0x0010,
    },
    ErrorEntry {
        feature: "F02",
        name: "SequenceGap",
        code: 0x0012,
    },
    ErrorEntry {
        feature: "F02",
        name: "Overflow",
        code: 0x001c,
    },
    ErrorEntry {
        feature: "F02",
        name: "NotFound",
        code: 0x0015,
    },
    ErrorEntry {
        feature: "F02",
        name: "ExecutionUnavailable",
        code: 0x001e,
    },
    ErrorEntry {
        feature: "F07",
        name: "NonCanonical",
        code: 0x0002,
    },
    ErrorEntry {
        feature: "F07",
        name: "UnsupportedVersion",
        code: 0x0001,
    },
    ErrorEntry {
        feature: "F07",
        name: "WrongDomain",
        code: 0x0003,
    },
    ErrorEntry {
        feature: "F07",
        name: "Unauthorized",
        code: 0x0006,
    },
    ErrorEntry {
        feature: "F07",
        name: "Overflow",
        code: 0x001c,
    },
    ErrorEntry {
        feature: "F08",
        name: "UnsupportedVersion",
        code: 0x0001,
    },
    ErrorEntry {
        feature: "F08",
        name: "NonCanonical",
        code: 0x0002,
    },
    ErrorEntry {
        feature: "F08",
        name: "WrongDomain",
        code: 0x0003,
    },
    ErrorEntry {
        feature: "F08",
        name: "WrongEpoch",
        code: 0x000c,
    },
    ErrorEntry {
        feature: "F08",
        name: "NotFound",
        code: 0x0015,
    },
    ErrorEntry {
        feature: "F08",
        name: "Overflow",
        code: 0x001c,
    },
];
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum OffchainSpace {
    WorkerService,
    ArtifactService,
    HistoryQuery,
    ViewProjection,
    ViewQuery,
}
pub fn offchain_error(space: OffchainSpace, code: u16) -> CodecResult<&'static str> {
    let table = match space {
        OffchainSpace::WorkerService => WORKER_SERVICE_ERRORS,
        OffchainSpace::ArtifactService => ARTIFACT_SERVICE_ERRORS,
        OffchainSpace::HistoryQuery => HISTORY_QUERY_ERRORS,
        OffchainSpace::ViewProjection => VIEW_PROJECTION_ERRORS,
        OffchainSpace::ViewQuery => VIEW_QUERY_ERRORS,
    };
    let index = code.checked_sub(1).ok_or(NON_CANONICAL)?;
    table.get(usize::from(index)).copied().ok_or(NON_CANONICAL)
}
pub const WORKER_SERVICE_ERRORS: &[&str] = &[
    "NonCanonical",
    "UnsupportedVersion",
    "WrongDomain",
    "BadSignature",
    "OwnerRequired",
    "DelegateConsentRequired",
    "DelegateRevoked",
    "IdentityFrozen",
    "StaleAuthority",
    "WrongGeneration",
    "WrongRevision",
    "MetadataExpired",
    "MetadataUnavailable",
    "MetadataIntegrityFailure",
    "CapabilityMismatch",
    "AdmissionNotEffective",
    "MarketPaused",
    "RateLimited",
    "CapacityExceeded",
    "InputTooLarge",
    "OutputTooLarge",
    "DeadlineInvalid",
    "IdempotencyConflict",
    "SequenceGap",
    "Overflow",
    "NotFound",
    "AccessDenied",
    "ExecutionUnavailable",
    "UnknownExecution",
];
pub const ARTIFACT_SERVICE_ERRORS: &[&str] = &[
    "Malformed",
    "UnsupportedVersion",
    "UnsupportedKind",
    "InvalidContext",
    "Unauthorized",
    "AuthorityRevoked",
    "PurposeDenied",
    "Expired",
    "QuotaExceeded",
    "CapacityUnavailable",
    "MissingChunk",
    "LengthMismatch",
    "RootMismatch",
    "SignatureInvalid",
    "IdempotencyConflict",
    "IntegrityConflict",
    "Tombstoned",
    "ContentUnavailable",
    "UnsafeLocator",
    "UnknownDelivery",
    "StorageFailure",
];
pub const HISTORY_QUERY_ERRORS: &[&str] = &["HistoryOutsideRetention"];
pub const VIEW_PROJECTION_ERRORS: &[&str] = &[
    "InvalidEncoding",
    "UnsupportedVersion",
    "WrongDomain",
    "IntegrityFailure",
    "BindingMismatch",
    "FinalityUnavailable",
    "SnapshotConflict",
    "ProjectionUnavailable",
    "CapacityExceeded",
];
pub const VIEW_QUERY_ERRORS: &[&str] = &[
    "CursorExpired",
    "CursorMismatch",
    "RateLimited",
    "SnapshotPruned",
    "ResponseTooLarge",
];
