//! Independent canonical bytes/hash fixtures constructed with Python hashlib.
//! No product execution produced expectations; no authority/balance/finality mocks.
use core::num::TryFromIntError;
use layerx_programs_ai_market::{codec::*, dispatch::*, errors::*, *};

enum Failure {
    Application(ApplicationError),
    Conversion(TryFromIntError),
    Unexpected(&'static str),
}
impl core::fmt::Debug for Failure {
    fn fmt(&self, f: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        match self {
            Self::Application(error) => write!(f, "application refusal {error:?}"),
            Self::Conversion(error) => write!(f, "integer conversion {error:?}"),
            Self::Unexpected(what) => write!(f, "unexpected {what}"),
        }
    }
}
impl From<ApplicationError> for Failure {
    fn from(error: ApplicationError) -> Self {
        Self::Application(error)
    }
}
impl From<TryFromIntError> for Failure {
    fn from(error: TryFromIntError) -> Self {
        Self::Conversion(error)
    }
}
type Checked<T = ()> = Result<T, Failure>;

fn hex(text: &str) -> Vec<u8> {
    assert_eq!(text.len() % 2, 0);
    text.as_bytes()
        .chunks_exact(2)
        .map(|p| {
            fn n(b: u8) -> u8 {
                match b {
                    b'0'..=b'9' => b - b'0',
                    b'a'..=b'f' => b - b'a' + 10,
                    _ => panic!("bad fixture"),
                }
            }
            n(p[0]) * 16 + n(p[1])
        })
        .collect()
}
fn fixed32(text: &str) -> Checked<[u8; 32]> {
    hex(text)
        .try_into()
        .map_err(|_| Failure::Unexpected("fixture is not 32 bytes"))
}
fn binding() -> Checked<EvaluatorBinding> {
    Ok(EvaluatorBinding {
        frozen: FrozenBinding {
            chain: ChainDomain::new([1; 32])?,
            program: ProgramId::new([2; 32])?,
            market: MarketId::new([3; 32])?,
            epoch: 0,
            config: Version::new(1)?,
            roster: RosterDigest::new([4; 32])?,
        },
        evaluator: EvaluatorId::new([5; 32])?,
        grant: Version::new(1)?,
        key_version: Version::new(1)?,
    })
}
fn score(id: u8, value: u32) -> Checked<ScoreEntry> {
    Ok(ScoreEntry {
        worker: WorkerId::new([id; 32])?,
        score: Score::new(value)?,
    })
}
fn common() -> Checked<EventCommon> {
    Ok(EventCommon {
        market: MarketId::new([3; 32])?,
        epoch: 0,
        config: Version::new(1)?,
        revision: 9,
        request: RequestDigest::new([8; 32])?,
        result: ResultDigest::new([9; 32])?,
    })
}
const NATIVE_BYTES:&str="50415841493100010a0101010101010101010101010101010101010101010101010101010101010101010202020202020202020202020202020202020202020202020202020202020202030303030303030303030303030303030303030303030303030303030303030308080808080808080808080808080808080808080808080808080808080808080000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000006409090909090909090909090909090909090909090909090909090909090909090000000000";
const SIGNED_BYTES:&str="5041584149310001020201010101010101010101010101010101010101010101010101010101010101010202020202020202020202020202020202020202020202020202020202020202030303030303030303030303030303030303030303030303030303030303030308080808080808080808080808080808080808080808080808080808080808080000000000000000000000000000000100000000000000000000000000000000000000000000000000000000000000000000000000000001000000000000006409090909090909090909090909090909090909090909090909090909090909090000004011111111111111111111111111111111111111111111111111111111111111111111111111111111111111111111111111111111111111111111111111111111010a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b";
const REPORT_BYTES:&str="00010101010101010101010101010101010101010101010101010101010101010101020202020202020202020202020202020202020202020202020202020202020203030303030303030303030303030303030303030303030303030303030303030000000000000000000000000000000104040404040404040404040404040404040404040404040404040404040404040505050505050505050505050505050505050505050505050505050505050505000000000000000100000000000000010606060606060606060606060606060606060606060606060606060606060606000107070707070707070707070707070707070707070707070707070707070707070000002a";
const WORKER_BYTES:&str="070707070707070707070707070707070707070707070707070707070707070708080808080808080808080808080808080808080808080808080808080808080909090909090909090909090909090909090909090909090909090909090909000000000000000100000000000000010a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b";
const EVALUATOR_BYTES:&str="05050505050505050505050505050505050505050505050505050505050505050c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c000000000000000100000000000000010d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e";
const ROSTER_BYTES:&str="00010303030303030303030303030303030303030303030303030303030303030303000000000000000000000000000000010001070707070707070707070707070707070707070707070707070707070707070708080808080808080808080808080808080808080808080808080808080808080909090909090909090909090909090909090909090909090909090909090909000000000000000100000000000000010a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b000105050505050505050505050505050505050505050505050505050505050505050c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c000000000000000100000000000000010d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e";
const STATE_BYTES:&str="50415841533100010000000000000001000100000000000000020000000000000003000000000000000400000000000000050000000000000006000000000000";
const COMMON_EVENT_BYTES:&str="0001030303030303030303030303030303030303030303030303030303030303030300000000000000000000000000000001000000000000000908080808080808080808080808080808080808080808080808080808080808080909090909090909090909090909090909090909090909090909090909090909";
const COMMIT_EVENT_BYTES:&str="000103030303030303030303030303030303030303030303030303030303030303030000000000000000000000000000000100000000000000090808080808080808080808080808080808080808080808080808080808080808090909090909090909090909090909090909090909090909090909090909090905050505050505050505050505050505050505050505050505050505050505050a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0000000000000040";
const REVEAL_EVENT_BYTES:&str="000103030303030303030303030303030303030303030303030303030303030303030000000000000000000000000000000100000000000000090808080808080808080808080808080808080808080808080808080808080808090909090909090909090909090909090909090909090909090909090909090905050505050505050505050505050505050505050505050505050505050505050b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b060606060606060606060606060606060606060606060606060606060606060600010000000000000050";
const SUCCESS_BYTES:&str="00010000000008080808080808080808080808080808080808080808080808080808080808080000000000000009a59e765498ed49dfed768fd32804d4aa99b9a474ba257c56637a9f784e61609f00000002aabb";
const ERROR_BYTES:&str="00010002001f000000000000000000000000000000000000000000000000000000000000000000000000000000000b1648d51b0821e61bc20834aa13f651130a194c2cc6ed502bdd9873f613a2cb00000000";
const NATIVE_DIGEST: &str = "e74aca951f411e8c39c0ddde138ed41d214ade297ab500212b87461fb09a79d2";
const SIGNED_DIGEST: &str = "69c17db00e7938a79bb7f8c6667600af7538b970de3fd8eb62c18493a95e607e";
const REPORT_DIGEST: &str = "8b384f6a87e0407872a2b3be7b1cf8d167a277c29780eb7c00c1847b61f52556";
const ATTESTATION_DIGEST: &str = "032b1a03e100166af1df9055ee30fea5f9f4f027132e70ee2eea3d09e4661710";
const COMMITMENT_DIGEST: &str = "a683a899fe44c20cff5eb54a145237877a88db5fb0dfff230ac234039f5a0a9e";
const ROSTER_DIGEST: &str = "07ed62feae3e15683d0eeea1ad0ce37fa6634915db7ec9820331a5d005f73864";
const STATE_DIGEST: &str = "b256df284079f0c5cb0495519764bcadc81b093554e2a50d5739b41f66de97e3";
const MARKET_ID: &str = "cd906cff12769a1cb7c9a15edcb163d0fd168682ca039fbeded8d3c50a0c4e01";
const WORKER_ID: &str = "31317a73cccb642878f2d56b7315abcd5516e83714e7cd4f80b98d5de5ef2a92";
const EVALUATOR_ID: &str = "d11dccece0993e13f0ddd68e9e00a58730214d7ea9d13e5ac3d9fb2b4719ba19";
const TASK_ID: &str = "30c87834cea0ac92e1cd6fd33a91edc30f4920977c9ae929a2885cd2bef25f8d";
const EMPTY_RESULT_DIGEST: &str =
    "0df71bd7bde81955acc617d2e804eea96a576dddb13333ec0e57dff967f51011";
const EMPTY_REQUEST_DIGEST: &str =
    "4a6843b45df48ddd0f33bda76bc0aabbb7deff2b86cf56e3a30e609b068e55ec";
const EXPECTED_OPERATIONS: &[(u16, &str)] = &[
    (0x0101, "CREATE"),
    (0x0102, "STAGE_POLICY"),
    (0x0103, "CANCEL_POLICY"),
    (0x0104, "SCHEDULE_ACTIVATION"),
    (0x0105, "ADVANCE_ACTIVATION"),
    (0x0106, "SUSPEND"),
    (0x0107, "UPDATE_METADATA"),
    (0x0108, "APPOINT_OPERATOR"),
    (0x0109, "REVOKE_OPERATOR"),
    (0x010A, "REQUEST_CLOSE"),
    (0x010B, "ADVANCE_CLOSE"),
    (0x010C, "ADMIT_TASK"),
    (0x010D, "ACCEPT_TASK"),
    (0x010E, "CANCEL_TASK"),
    (0x010F, "COMMIT_TASK_RESULT"),
    (0x0110, "SEAL_TASK_SET"),
    (0x0111, "OPEN_EPOCH"),
    (0x0201, "EnrollWorker"),
    (0x0202, "PublishMetadata"),
    (0x0203, "SetDraining"),
    (0x0204, "UndoDrain"),
    (0x0205, "RotateDelegate"),
    (0x0206, "RevokeDelegate"),
    (0x0207, "RetireWorker"),
    (0x0209, "AcceptEnrollment"),
    (0x020A, "ExpireEnrollment"),
    (0x0301, "ScheduleEvaluator"),
    (0x0302, "RotateEvaluatorKey"),
    (0x0303, "RevokeEvaluator"),
    (0x0304, "ChallengeAssessment"),
    (0x0401, "CommitScore"),
    (0x0402, "RevealScore"),
    (0x0501, "BeginAggregation"),
    (0x0502, "ProcessAggregation"),
    (0x0503, "FinalizeAggregation"),
    (0x0601, "FUND"),
    (0x0602, "CLAIM"),
    (0x0603, "EXPIRE_EPOCH_CLAIMS"),
    (0x0604, "REFUND_FREE"),
    (0x0605, "PRUNE_EPOCH"),
    (0x0701, "ResetHistory"),
    (0x0702, "SuspendHistory"),
    (0x0703, "ResumeHistory"),
    (0x0801, "ApproveAdmission"),
    (0x0802, "RevokeAdmissionApproval"),
    (0x0803, "AdmitWorker"),
    (0x0804, "AdmitEvaluator"),
    (0x0805, "Heartbeat"),
    (0x0806, "RequestExit"),
    (0x0807, "CancelExit"),
    (0x0808, "PruneInactive"),
    (0x0809, "AdministrativeRemove"),
    (0x0901, "SealEvidence"),
    (0x0A01, "READ_HEADER"),
    (0x0A02, "READ_STATE_CHUNK"),
];
const EXPECTED_ERRORS: &[(&str, &str, u16)] = &[
    ("COMMON", "BAD_VERSION", 0x0001),
    ("COMMON", "NON_CANONICAL", 0x0002),
    ("COMMON", "WRONG_DOMAIN", 0x0003),
    ("COMMON", "WRONG_PROGRAM", 0x0004),
    ("COMMON", "WRONG_MARKET", 0x0005),
    ("COMMON", "UNAUTHORIZED", 0x0006),
    ("COMMON", "ROLE_CONFLICT", 0x0007),
    ("COMMON", "REVOKED", 0x0008),
    ("COMMON", "KEY_MISMATCH", 0x0009),
    ("COMMON", "BAD_SIGNATURE", 0x000a),
    ("COMMON", "EXPIRED", 0x000b),
    ("COMMON", "WRONG_EPOCH", 0x000c),
    ("COMMON", "WRONG_CONFIG", 0x000d),
    ("COMMON", "WRONG_ROSTER", 0x000e),
    ("COMMON", "WRONG_PHASE", 0x000f),
    ("COMMON", "REPLAY_CONFLICT", 0x0010),
    ("COMMON", "SEQUENCE_CONSUMED", 0x0011),
    ("COMMON", "SEQUENCE_GAP", 0x0012),
    ("COMMON", "STALE_CURSOR", 0x0013),
    ("COMMON", "CONFLICT", 0x0014),
    ("COMMON", "NOT_FOUND", 0x0015),
    ("COMMON", "CAPACITY", 0x0016),
    ("COMMON", "RETENTION_FULL", 0x0017),
    ("COMMON", "INSUFFICIENT_FREE", 0x0018),
    ("COMMON", "ACCOUNT_BINDING", 0x0019),
    ("COMMON", "HOST_CAPABILITY", 0x001a),
    ("COMMON", "HOST_TRANSFER", 0x001b),
    ("COMMON", "ARITHMETIC", 0x001c),
    ("COMMON", "EVIDENCE_BINDING", 0x001d),
    ("COMMON", "READINESS_BLOCKED", 0x001e),
    ("COMMON", "UNKNOWN_OPERATION", 0x001f),
    ("F03", "NO_GRANT", 0x0301),
    ("F03", "EVALUATOR_CAPACITY", 0x0302),
    ("F03", "BAD_ACTIVATION", 0x0303),
    ("F03", "KEY_REUSED", 0x0304),
    ("F03", "KEY_VERSION_CONFLICT", 0x0305),
    ("F03", "GRANT_VERSION_CONFLICT", 0x0306),
    ("F03", "REPORT_ALREADY_FINAL", 0x0307),
    ("F03", "CHALLENGE_CAPACITY", 0x0308),
    ("F03", "UNKNOWN_WORKER", 0x0309),
    ("F03", "NO_SCORES", 0x030a),
    ("F03", "SCORE_RANGE", 0x030b),
    ("F03", "NONCANONICAL_VECTOR", 0x030c),
    ("F03", "EVIDENCE_NOT_SEALED", 0x030d),
    ("F03", "EVIDENCE_ROOT_MISMATCH", 0x030e),
    ("F06", "FUNDING_POLICY_MISMATCH", 0x0601),
    ("F06", "REFUND_RECIPIENT_MISMATCH", 0x0602),
    ("F06", "WRONG_ASSET", 0x0603),
    ("F06", "INVALID_AMOUNT", 0x0604),
    ("F06", "EPOCH_ALREADY_RESERVED", 0x0605),
    ("F06", "EPOCH_NOT_RESERVED", 0x0606),
    ("F06", "AGGREGATION_MISMATCH", 0x0607),
    ("F06", "EPOCH_TERMINAL", 0x0608),
    ("F06", "CLAIM_NOT_READY", 0x0609),
    ("F06", "CLAIM_EXPIRED", 0x060a),
    ("F06", "UNKNOWN_WORKER_ENTITLEMENT", 0x060b),
    ("F06", "WRONG_CLAIM_RECIPIENT", 0x060c),
    ("F06", "WRONG_CLAIM_AMOUNT", 0x060d),
    ("F06", "NOTHING_TO_CLAIM", 0x060e),
    ("F06", "LEDGER_INVARIANT_VIOLATION", 0x060f),
    ("F06", "CONTRIBUTION_CONSENT_REQUIRED", 0x0610),
    ("F01", "InvalidPolicy", 0x0101),
    ("F01", "PrincipalMismatch", 0x0102),
    ("F01", "StaleRevision", 0x0103),
    ("F01", "AlreadyCreated", 0x0104),
    ("F01", "AccountBindingMissing", 0x0105),
    ("F01", "VersionMismatch", 0x0106),
    ("F01", "PendingPolicyExists", 0x0107),
    ("F01", "NoPendingPolicy", 0x0108),
    ("F01", "ActivationTooEarly", 0x0109),
    ("F01", "ActivationNotReady", 0x010a),
    ("F01", "AlreadyActivated", 0x010b),
    ("F01", "WrongLifecycle", 0x010c),
    ("F01", "LifecycleClosed", 0x010d),
    ("F01", "InvalidReason", 0x010e),
    ("F01", "GrantAlreadyRevoked", 0x010f),
    ("F01", "AlreadyClosing", 0x0110),
    ("F01", "ObligationsOutstanding", 0x0111),
    ("F01", "CapacityUnavailable", 0x0112),
    ("F01", "NoTaskCapacity", 0x0113),
    ("F01", "UnknownWorker", 0x0114),
    ("F01", "TaskConflict", 0x0115),
    ("F01", "TaskExpired", 0x0116),
    ("F01", "PolicyMismatch", 0x0117),
    ("F01", "TaskNotFound", 0x0118),
    ("F01", "WrongWorker", 0x0119),
    ("F01", "TaskAlreadyAccepted", 0x011a),
    ("F02", "OwnerRequired", 0x0201),
    ("F02", "DelegateConsentRequired", 0x0202),
    ("F02", "DelegateRevoked", 0x0203),
    ("F02", "IdentityFrozen", 0x0204),
    ("F02", "StaleAuthority", 0x0205),
    ("F02", "WrongGeneration", 0x0206),
    ("F02", "WrongRevision", 0x0207),
    ("F02", "MetadataExpired", 0x0208),
    ("F02", "MetadataUnavailable", 0x0209),
    ("F02", "MetadataIntegrityFailure", 0x020a),
    ("F02", "CapabilityMismatch", 0x020b),
    ("F02", "AdmissionNotEffective", 0x020c),
    ("F02", "MarketPaused", 0x020d),
    ("F02", "RateLimited", 0x020e),
    ("F02", "InputTooLarge", 0x020f),
    ("F02", "OutputTooLarge", 0x0210),
    ("F02", "DeadlineInvalid", 0x0211),
    ("F02", "AccessDenied", 0x0212),
    ("F02", "UnknownExecution", 0x0213),
    ("F04", "F04_NO_SCORES", 0x0401),
    ("F04", "F04_SALT_INVALID", 0x0402),
    ("F04", "F04_COMMIT_MISMATCH", 0x0403),
    ("F05", "F05_REPORT_INVARIANT", 0x0501),
    ("F07", "IdentityFrozen", 0x0701),
    ("F07", "SegmentMismatch", 0x0702),
    ("F07", "GenerationMismatch", 0x0703),
    ("F07", "UnknownWorker", 0x0704),
    ("F07", "EpochNotSealed", 0x0705),
    ("F07", "BindingMismatch", 0x0706),
    ("F07", "ResourceLimit", 0x0707),
    ("F07", "FinalityUnavailable", 0x0708),
    ("F08", "OwnerRequired", 0x0801),
    ("F08", "AdministratorApprovalRequired", 0x0802),
    ("F08", "BadConsent", 0x0803),
    ("F08", "PermitExpired", 0x0804),
    ("F08", "PermitConsumed", 0x0805),
    ("F08", "IdentityFrozen", 0x0806),
    ("F08", "DelegateRevoked", 0x0807),
    ("F08", "WrongGeneration", 0x0808),
    ("F08", "MarketPaused", 0x0809),
    ("F08", "DuplicateIdentity", 0x080a),
    ("F08", "OwnerCapacityExceeded", 0x080b),
    ("F08", "CapacityExceeded", 0x080c),
    ("F08", "AdmissionWindowFull", 0x080d),
    ("F08", "RateLimited", 0x080e),
    ("F08", "NoPrunableMember", 0x080f),
    ("F08", "CandidateChanged", 0x0810),
    ("F08", "StaleState", 0x0811),
    ("F08", "RetentionBlocked", 0x0812),
    ("F08", "QuorumUnavailable", 0x0813),
    ("F08", "IdempotencyConflict", 0x0814),
    ("F08", "ResourceExhausted", 0x0815),
    ("F09", "EVIDENCE_TASK_SET_UNSEALED", 0x0901),
    ("F09", "EVIDENCE_SEAL_CONFLICT", 0x0902),
    ("F07", "IdempotencyConflict", 0x0709),
];
const EXPECTED_ALIASES: &[(&str, &str, u16)] = &[
    ("F01", "InvalidEncoding", 0x0002),
    ("F01", "WrongDomain", 0x0003),
    ("F01", "Unauthorized", 0x0006),
    ("F01", "ArithmeticOverflow", 0x001c),
    ("F02", "NonCanonical", 0x0002),
    ("F02", "UnsupportedVersion", 0x0001),
    ("F02", "WrongDomain", 0x0003),
    ("F02", "BadSignature", 0x000a),
    ("F02", "CapacityExceeded", 0x0016),
    ("F02", "IdempotencyConflict", 0x0010),
    ("F02", "SequenceGap", 0x0012),
    ("F02", "Overflow", 0x001c),
    ("F02", "NotFound", 0x0015),
    ("F02", "ExecutionUnavailable", 0x001e),
    ("F07", "NonCanonical", 0x0002),
    ("F07", "UnsupportedVersion", 0x0001),
    ("F07", "WrongDomain", 0x0003),
    ("F07", "Unauthorized", 0x0006),
    ("F07", "Overflow", 0x001c),
    ("F08", "UnsupportedVersion", 0x0001),
    ("F08", "NonCanonical", 0x0002),
    ("F08", "WrongDomain", 0x0003),
    ("F08", "WrongEpoch", 0x000c),
    ("F08", "NotFound", 0x0015),
    ("F08", "Overflow", 0x001c),
];

#[test]
fn primitives_versions_ordering_and_decimal_boundaries() -> Checked {
    assert!(PrincipalId::new([0; 32]).is_err());
    assert!(ProgramId::new([0; 32]).is_err());
    assert!(MarketId::new([0; 32]).is_err());
    assert!(WorkerId::new([0; 32]).is_err());
    assert!(EvaluatorId::new([0; 32]).is_err());
    assert!(AccountId::new([0; 32]).is_err());
    assert!(AssetId::new([0; 32]).is_err());
    assert!(RequestId::new([0; 32]).is_err());
    assert!(TaskId::new([0; 32]).is_err());
    assert!(Digest32::new([0; 32]).is_err());
    assert!(Version::new(0).is_err());
    assert_eq!(Version::new(u64::MAX)?.next(), Err(ARITHMETIC));
    assert!(WorkerId::new([1; 32])? < WorkerId::new([2; 32])?);
    assert_eq!(Score::new(0)?.get(), 0);
    assert_eq!(Score::new(1_000_000)?.get(), 1_000_000);
    assert_eq!(Score::new(1_000_001), Err(F03_SCORE_RANGE));
    assert_eq!(decimal_u64("18446744073709551615"), Ok(u64::MAX));
    assert_eq!(decimal_u64("18446744073709551616"), Err(ARITHMETIC));
    assert_eq!(
        decimal_u128("340282366920938463463374607431768211455"),
        Ok(u128::MAX)
    );
    assert_eq!(
        decimal_u128("340282366920938463463374607431768211456"),
        Err(ARITHMETIC)
    );
    assert_eq!(decimal_u128("0"), Ok(0));
    for text in ["", "00", "01", "+1", "-1", " 1", "1 ", "1.0", "１"] {
        assert!(decimal_u128(text).is_err());
    }
    let mut bytes = [0; 31];
    let mut w = Writer::new(&mut bytes);
    w.u8(1)?;
    w.u16(0x0203)?;
    w.u32(0x0405_0607)?;
    w.u64(0x0809_0a0b_0c0d_0e0f)?;
    w.u128(0x1011_1213_1415_1617_1819_1a1b_1c1d_1e1f)?;
    assert_eq!(
        bytes.to_vec(),
        hex("0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f")
    );
    let mut r = Reader::new(&bytes);
    assert_eq!(r.u8()?, 1);
    assert_eq!(r.u16()?, 0x0203);
    assert_eq!(r.u32()?, 0x0405_0607);
    assert_eq!(r.u64()?, 0x0809_0a0b_0c0d_0e0f);
    assert_eq!(r.u128()?, 0x1011_1213_1415_1617_1819_1a1b_1c1d_1e1f);
    r.finish()?;
    assert!(Reader::new(&[2]).boolean().is_err());
    assert!(Reader::new(&[2]).presence(Reader::u64).is_err());
    assert!(Reader::new(&[0, 1]).reserved(2).is_err());
    assert!(Reader::new(&[0]).finish().is_err());
    let mut r = Reader::new(&[0]);
    r.take(1)?;
    assert_eq!(r.take(usize::MAX), Err(ARITHMETIC));
    assert_eq!(Reader::new(&[255, 255, 255, 255]).bytes(16), Err(CAPACITY));
    assert_eq!(
        Reader::new(&[255, 255]).vector_bytes(0, 32, usize::MAX),
        Err(CAPACITY)
    );
    assert_eq!(
        Reader::new(&[0, 2]).vector_bytes(0, 32, usize::MAX),
        Err(ARITHMETIC)
    );
    assert_eq!(Salt32::new([0; 32]), Err(F04_SALT_INVALID));
    Ok(())
}

#[test]
fn all_fifty_five_selectors_and_entire_error_freeze() -> Checked {
    assert_eq!(EXPECTED_OPERATIONS.len(), 55);
    assert_eq!(OPERATIONS.len(), 55);
    for &(code, name) in EXPECTED_OPERATIONS {
        let op = Operation::decode(code)?;
        assert_eq!(op.selector(), code);
        assert_eq!(op.metadata().name, name);
        assert_eq!(op.metadata().feature, u8::try_from(code >> 8)?);
    }
    for code in [
        0, 1, 255, 0x0100, 0x0112, 0x0208, 0x0281, 0x0305, 0x0704, 0x0902, 0x0a03, 0xffff,
    ] {
        assert_eq!(Operation::decode(code), Err(UNKNOWN_OPERATION));
    }
    assert_eq!(APPLICATION_ERRORS.len(), EXPECTED_ERRORS.len());
    assert_eq!(APPLICATION_ALIASES.len(), EXPECTED_ALIASES.len());
    for &(feature, name, code) in EXPECTED_ERRORS {
        assert_eq!(ApplicationError::from_code(code)?.code(), code);
        assert_eq!(ApplicationError::named(feature, name)?.code(), code);
    }
    for &(feature, name, code) in EXPECTED_ALIASES {
        assert_eq!(ApplicationError::named(feature, name)?.code(), code);
    }
    for (i, a) in APPLICATION_ERRORS.iter().enumerate() {
        assert!(APPLICATION_ERRORS[i + 1..].iter().all(|b| b.code != a.code));
    }
    for code in [0, 0x0020, 0x030f, 0x0611, 0xffff] {
        assert!(ApplicationError::from_code(code).is_err());
    }
    assert_eq!(F03_NO_SCORES.code(), 0x030a);
    assert_eq!(F06_CONTRIBUTION_CONSENT_REQUIRED.code(), 0x0610);
    assert_eq!(F09_EVIDENCE_TASK_SET_UNSEALED.code(), 0x0901);
    assert_eq!(F09_EVIDENCE_SEAL_CONFLICT.code(), 0x0902);
    for (space, table) in [
        (OffchainSpace::WorkerService, WORKER_SERVICE_ERRORS),
        (OffchainSpace::ArtifactService, ARTIFACT_SERVICE_ERRORS),
        (OffchainSpace::HistoryQuery, HISTORY_QUERY_ERRORS),
        (OffchainSpace::ViewProjection, VIEW_PROJECTION_ERRORS),
        (OffchainSpace::ViewQuery, VIEW_QUERY_ERRORS),
    ] {
        assert!(offchain_error(space, 0).is_err());
        assert!(offchain_error(space, u16::MAX).is_err());
        for (i, name) in table.iter().enumerate() {
            assert_eq!(offchain_error(space, u16::try_from(i + 1)?), Ok(*name));
        }
    }
    assert_eq!(
        offchain_error(OffchainSpace::WorkerService, 1),
        Ok("NonCanonical")
    );
    assert_eq!(
        offchain_error(OffchainSpace::ArtifactService, 1),
        Ok("Malformed")
    );
    assert_eq!(
        offchain_error(OffchainSpace::HistoryQuery, 1),
        Ok("HistoryOutsideRetention")
    );
    assert_eq!(
        offchain_error(OffchainSpace::ViewProjection, 1),
        Ok("InvalidEncoding")
    );
    assert_eq!(
        offchain_error(OffchainSpace::ViewQuery, 1),
        Ok("CursorExpired")
    );
    Ok(())
}

#[test]
fn envelope_fixed_native_delegate_offsets_hashes_and_refusals() -> Checked {
    let bytes = hex(NATIVE_BYTES);
    assert_eq!(bytes.len(), 239);
    let v = decode_envelope(&bytes)?;
    assert_eq!(v.unsigned_bytes().len(), 238);
    assert_eq!(v.request_digest()?.bytes(), fixed32(NATIVE_DIGEST)?);
    assert_eq!(v.envelope.authentication, Authentication::Native);
    let mut out = [0; 512];
    assert_eq!(encode_envelope(&v.envelope, &mut out)?, 239);
    assert_eq!(&out[..239], bytes.as_slice());
    assert_eq!(v.envelope.check_expiry(99), Ok(()));
    assert_eq!(v.envelope.check_expiry(100), Err(EXPIRED));
    assert_eq!(v.envelope.check_expiry(101), Err(EXPIRED));
    assert_eq!(
        request_signing_message(v.request_digest()?),
        fixed32(NATIVE_DIGEST)?
    );
    for n in 0..bytes.len() {
        assert!(decode_envelope(&bytes[..n]).is_err());
    }
    let mut bad = bytes.clone();
    bad.push(0);
    assert!(decode_envelope(&bad).is_err());
    for (offset, value) in [(6, 1), (9, 3), (238, 2), (201, 0)] {
        let mut bad = bytes.clone();
        bad[offset] = value;
        assert!(decode_envelope(&bad).is_err());
    }
    let mut bad = bytes.clone();
    bad[234..238].copy_from_slice(&u32::MAX.to_be_bytes());
    assert!(decode_envelope(&bad).is_err());
    let mut bad = bytes.clone();
    bad[170] = 1;
    assert!(decode_envelope(&bad).is_err());
    let mut bad = bytes.clone();
    bad[202..234].fill(0);
    assert!(decode_envelope(&bad).is_err());
    let signed = hex(SIGNED_BYTES);
    assert_eq!(signed.len(), 399);
    let s = decode_envelope(&signed)?;
    assert_eq!(s.unsigned_bytes().len(), 302);
    assert_eq!(s.request_digest()?.bytes(), fixed32(SIGNED_DIGEST)?);
    assert_eq!(signed[302], 1);
    assert_eq!(&signed[303..335], &[10; 32]);
    assert_eq!(&signed[335..399], &[11; 64]);
    assert_eq!(encode_envelope(&s.envelope, &mut out)?, 399);
    assert_eq!(&out[..399], signed.as_slice());
    for n in 302..399 {
        assert!(decode_envelope(&signed[..n]).is_err());
    }
    let mut changed = signed.clone();
    changed[398] ^= 1;
    assert_eq!(
        decode_envelope(&changed)?.request_digest()?,
        s.request_digest()?
    );
    let mut changed = signed.clone();
    changed[301] ^= 1;
    assert_ne!(
        decode_envelope(&changed)?.request_digest()?,
        s.request_digest()?
    );
    let mut no_sequence = s.envelope;
    no_sequence.sequence = 0;
    assert!(no_sequence.validate().is_err());
    let mut read_delegate = v.envelope;
    read_delegate.authentication = s.envelope.authentication;
    assert!(read_delegate.validate().is_err());
    assert_eq!(
        compare_direct_call(Presence::Present(ProgramId::new([2; 32])?)),
        Err(UNAUTHORIZED)
    );
    assert_eq!(compare_direct_call(Presence::Absent), Ok(()));
    assert_eq!(
        compare_native_principal(&v.envelope, PrincipalId::new([1; 32])?),
        Err(UNAUTHORIZED)
    );
    assert_eq!(
        v.envelope.check_domain(
            ChainDomain::new([2; 32])?,
            ProgramId::new([2; 32])?,
            MarketId::new([3; 32])?
        ),
        Err(WRONG_DOMAIN)
    );
    assert!(decode_envelope(&vec![0; 16385]).is_err());
    let huge = vec![1; 15966];
    let mut e = s.envelope;
    e.payload = &huge;
    assert!(e.validate().is_err());
    Ok(())
}

#[test]
fn independent_report_attestation_commitment_vectors_and_set_refusals() -> Checked {
    let scores = [score(7, 42)?];
    let report = ReportBody {
        binding: binding()?,
        evidence: EvidenceRoot::new([6; 32])?,
        scores: ScoreVector::Typed(&scores),
    };
    let mut out = [0; 1381];
    let n = encode_report(&report, &mut out)?;
    assert_eq!(n, 264);
    assert_eq!(&out[..n], hex(REPORT_BYTES).as_slice());
    let digest = report_digest(&report)?;
    assert_eq!(digest.bytes(), fixed32(REPORT_DIGEST)?);
    let attestation = attestation_digest(digest)?;
    assert_eq!(attestation.bytes(), fixed32(ATTESTATION_DIGEST)?);
    assert_eq!(
        report_signing_message(attestation),
        fixed32(ATTESTATION_DIGEST)?
    );
    assert_eq!(
        commitment_digest(&binding()?, digest, Salt32::new([15; 32])?)?.bytes(),
        fixed32(COMMITMENT_DIGEST)?
    );
    assert_ne!(digest.bytes(), attestation.bytes());
    assert_ne!(digest.bytes(), fixed32(COMMITMENT_DIGEST)?);
    let input = hex(REPORT_BYTES);
    let decoded = decode_report(&input)?;
    assert_eq!(decoded.binding, binding()?);
    assert_eq!(
        decoded
            .scores
            .entries()
            .next()
            .ok_or(Failure::Unexpected("decoded report has no score entry"))??,
        scores[0]
    );
    for n in 0..input.len() {
        assert!(decode_report(&input[..n]).is_err());
    }
    let mut trailing = input.clone();
    trailing.push(0);
    assert!(decode_report(&trailing).is_err());
    let mut empty = input[..228].to_vec();
    empty[226..228].copy_from_slice(&0_u16.to_be_bytes());
    assert_eq!(decode_report(&empty).err(), Some(F03_NO_SCORES));
    let mut too_many = input.clone();
    too_many[226..228].copy_from_slice(&33_u16.to_be_bytes());
    assert!(decode_report(&too_many).is_err());
    let mut bad = input.clone();
    bad[260..264].copy_from_slice(&1_000_001_u32.to_be_bytes());
    assert_eq!(decode_report(&bad).err(), Some(F03_SCORE_RANGE));
    for values in [
        vec![score(7, 0)?, score(7, 1)?],
        vec![score(8, 1)?, score(7, 1)?],
    ] {
        let v = ReportBody {
            scores: ScoreVector::Typed(&values),
            ..report
        };
        assert!(encode_report(&v, &mut out).is_err());
    }
    assert_eq!(ScoreVector::Typed(&[]).validate(), Err(F03_NO_SCORES));
    assert!(ScoreVector::Typed(&vec![score(7, 0)?; 33])
        .validate()
        .is_err());
    let many: Vec<_> = (1..=32).map(|i| score(i, 0)).collect::<Checked<_>>()?;
    let v = ReportBody {
        scores: ScoreVector::Typed(&many),
        ..report
    };
    assert_eq!(encode_report(&v, &mut out)?, 1380);
    decode_report(&out[..1380])?;
    let c = CommitScorePayload {
        binding: binding()?,
        commitment: CommitmentDigest::new(fixed32(COMMITMENT_DIGEST)?)?,
    };
    let mut bytes = [0; 224];
    assert_eq!(encode_commit_score(&c, &mut bytes)?, 224);
    assert_eq!(&bytes[..192], &input[2..194]);
    assert_eq!(decode_commit_score(&bytes)?, c);
    let reveal = RevealScorePayload {
        report,
        signature: Signature64([11; 64]),
        salt: Salt32::new([15; 32])?,
    };
    let mut bytes = [0; 1480];
    assert_eq!(encode_reveal_score(&reveal, &mut bytes)?, 364);
    assert_eq!(&bytes[..4], &264_u32.to_be_bytes());
    assert_eq!(&bytes[4..268], input.as_slice());
    decode_reveal_score(&bytes[..364])?;
    bytes[332..364].fill(0);
    assert_eq!(
        decode_reveal_score(&bytes[..364]).err(),
        Some(F04_SALT_INVALID)
    );
    Ok(())
}

#[test]
fn independent_id_derivation_and_cross_domain_separation() -> Checked {
    let chain = ChainDomain::new([1; 32])?;
    let program = ProgramId::new([2; 32])?;
    let market = MarketId::new([3; 32])?;
    let owner = PrincipalId::new([8; 32])?;
    assert_eq!(derive_market(chain, program)?.bytes(), fixed32(MARKET_ID)?);
    assert_eq!(
        derive_worker(market, owner, [15; 32])?.bytes(),
        fixed32(WORKER_ID)?
    );
    assert_eq!(
        derive_evaluator(market, owner, [15; 32])?.bytes(),
        fixed32(EVALUATOR_ID)?
    );
    assert_eq!(
        derive_task(market, 0, owner, [15; 32])?.bytes(),
        fixed32(TASK_ID)?
    );
    assert_eq!(
        domain_hash("PAXAI/result/v1", &[])?.bytes(),
        fixed32(EMPTY_RESULT_DIGEST)?
    );
    assert_eq!(
        domain_hash("PAXAI/request/v1", &[])?.bytes(),
        fixed32(EMPTY_REQUEST_DIGEST)?
    );
    assert_ne!(EMPTY_RESULT_DIGEST, EMPTY_REQUEST_DIGEST);
    for domain in ["", "non\0ascii", "é"] {
        assert!(domain_hash(domain, &[]).is_err());
    }
    assert_eq!(
        refuse_identity_collision(&[1; 32], &[[1; 32]]),
        Err(CONFLICT)
    );
    assert!(refuse_identity_collision(&[0; 32], &[]).is_err());
    assert_eq!(refuse_identity_collision(&[1; 32], &[[2; 32]]), Ok(()));
    Ok(())
}

#[test]
fn exact_roster_entries_preimage_hash_and_order() -> Checked {
    let worker = WorkerRosterEntry {
        worker: WorkerId::new([7; 32])?,
        owner: PrincipalId::new([8; 32])?,
        recipient: AccountId::new([9; 32])?,
        generation: Version::new(1)?,
        key_version: Version::new(1)?,
        public_key: PublicKey32([10; 32]),
        metadata: MetadataDigest::new([11; 32])?,
    };
    let evaluator = EvaluatorRosterEntry {
        evaluator: EvaluatorId::new([5; 32])?,
        owner: PrincipalId::new([12; 32])?,
        grant: Version::new(1)?,
        key_version: Version::new(1)?,
        public_key: PublicKey32([13; 32]),
        rubric: RubricDigest::new([14; 32])?,
    };
    let mut wb = [0; 176];
    assert_eq!(encode_worker_roster(&worker, &mut wb)?, 176);
    assert_eq!(wb.to_vec(), hex(WORKER_BYTES));
    let mut eb = [0; 144];
    assert_eq!(encode_evaluator_roster(&evaluator, &mut eb)?, 144);
    assert_eq!(eb.to_vec(), hex(EVALUATOR_BYTES));
    let workers = [worker];
    let evaluators = [evaluator];
    let roster = Roster {
        market: MarketId::new([3; 32])?,
        epoch: 0,
        config: Version::new(1)?,
        workers: &workers,
        evaluators: &evaluators,
    };
    let mut out = [0; ROSTER_MAX_BYTES];
    assert_eq!(encode_roster(&roster, &mut out)?, 374);
    assert_eq!(&out[..374], hex(ROSTER_BYTES).as_slice());
    assert_eq!(roster_digest(&roster)?.bytes(), fixed32(ROSTER_DIGEST)?);
    let input = hex(ROSTER_BYTES);
    let view = decode_roster(&input)?;
    assert_eq!(view.worker(0)?, worker);
    assert_eq!(view.evaluator(0)?, evaluator);
    assert_eq!(view.digest()?.bytes(), fixed32(ROSTER_DIGEST)?);
    assert!(view.worker(usize::MAX).is_err());
    assert!(view.evaluator(1).is_err());
    let duplicate = [worker, worker];
    assert!(Roster {
        workers: &duplicate,
        ..roster
    }
    .validate()
    .is_err());
    let mut other = worker;
    other.worker = WorkerId::new([6; 32])?;
    let descending = [worker, other];
    assert!(Roster {
        workers: &descending,
        ..roster
    }
    .validate()
    .is_err());
    let duplicate = [evaluator, evaluator];
    assert!(Roster {
        evaluators: &duplicate,
        ..roster
    }
    .validate()
    .is_err());
    let mut bad = input.clone();
    bad[50..52].copy_from_slice(&33_u16.to_be_bytes());
    assert!(decode_roster(&bad).is_err());
    let mut bad = input.clone();
    bad[228..230].copy_from_slice(&9_u16.to_be_bytes());
    assert!(decode_roster(&bad).is_err());
    let mut bad = input.clone();
    bad.push(0);
    assert!(decode_roster(&bad).is_err());
    Ok(())
}

#[test]
fn exact_result_header_status_known_error_empty_failure_and_digest() -> Checked {
    let payload = [0xaa, 0xbb];
    let result =
        ApplicationResult::success(ResultStatus::Ok, RequestDigest::new([8; 32])?, 9, &payload)?;
    let mut out = vec![0; 16385];
    assert_eq!(encode_result(&result, &mut out)?, 84);
    assert_eq!(&out[..84], hex(SUCCESS_BYTES).as_slice());
    assert_eq!(&out[78..82], &2_u32.to_be_bytes());
    let error = ApplicationResult::failure(UNKNOWN_OPERATION, Presence::Absent, 99)?;
    assert_eq!(error.revision, 0);
    assert_eq!(encode_result(&error, &mut out)?, 82);
    assert_eq!(&out[..82], hex(ERROR_BYTES).as_slice());
    let input = hex(ERROR_BYTES);
    decode_result(&input)?;
    let success = hex(SUCCESS_BYTES);
    decode_result(&success)?;
    for n in 0..success.len() {
        assert!(decode_result(&success[..n]).is_err());
    }
    for (offset, value) in [(3, 3), (5, 1), (46, 0), (81, 3)] {
        let mut bad = success.clone();
        bad[offset] = value;
        assert!(decode_result(&bad).is_err());
    }
    let mut bad = input.clone();
    bad[4..6].fill(0);
    assert!(decode_result(&bad).is_err());
    let mut bad = input.clone();
    bad[78..82].copy_from_slice(&1_u32.to_be_bytes());
    bad.push(0);
    assert!(decode_result(&bad).is_err());
    let mut bad = input.clone();
    bad[4..6].copy_from_slice(&0xffff_u16.to_be_bytes());
    assert!(decode_result(&bad).is_err());
    let mut bad = input.clone();
    bad.push(0);
    assert!(decode_result(&bad).is_err());
    let already = ApplicationResult::success(
        ResultStatus::AlreadyApplied,
        RequestDigest::new([8; 32])?,
        9,
        &payload,
    )?;
    assert_eq!(already.digest, result.digest);
    assert_eq!(already.revision, result.revision);
    assert_eq!(encode_result(&already, &mut out)?, 84);
    assert_eq!(&out[2..6], &[0, 1, 0, 0]);
    let unauthorized = ApplicationResult::failure(
        UNAUTHORIZED,
        Presence::Present(RequestDigest::new([8; 32])?),
        9,
    )?;
    assert_eq!(unauthorized.revision, 0);
    let visible =
        ApplicationResult::failure(CONFLICT, Presence::Present(RequestDigest::new([8; 32])?), 9)?;
    assert_eq!(visible.revision, 9);
    let maximum = vec![1; 16302];
    let r =
        ApplicationResult::success(ResultStatus::Ok, RequestDigest::new([8; 32])?, 9, &maximum)?;
    assert_eq!(encode_result(&r, &mut out)?, 16384);
    assert!(ApplicationResult::success(
        ResultStatus::Ok,
        RequestDigest::new([8; 32])?,
        9,
        &vec![1; 16303]
    )
    .is_err());
    Ok(())
}

#[test]
fn exact_common_commit_reveal_events_and_bounds() -> Checked {
    let mut out = [0; 2049];
    assert_eq!(encode_event_common(&common()?, &mut out)?, 122);
    assert_eq!(&out[..122], hex(COMMON_EVENT_BYTES).as_slice());
    let commit = CommitAccepted {
        common: common()?,
        evaluator: EvaluatorId::new([5; 32])?,
        commitment: CommitmentDigest::new([10; 32])?,
        accepted_height: 64,
    };
    assert_eq!(encode_commit_event(&commit, &mut out)?, 194);
    assert_eq!(&out[..194], hex(COMMIT_EVENT_BYTES).as_slice());
    let input = hex(COMMIT_EVENT_BYTES);
    assert_eq!(decode_commit_event(&input)?, commit);
    let reveal = RevealScoreEvent {
        common: common()?,
        evaluator: EvaluatorId::new([5; 32])?,
        report: ReportDigest::new([11; 32])?,
        evidence: EvidenceRoot::new([6; 32])?,
        vector_count: 1,
        admitted_height: 80,
    };
    assert_eq!(encode_reveal_event(&reveal, &mut out)?, 228);
    assert_eq!(&out[..228], hex(REVEAL_EVENT_BYTES).as_slice());
    let input = hex(REVEAL_EVENT_BYTES);
    assert_eq!(decode_reveal_event(&input)?, reveal);
    let mut topic = [0; 64];
    let n = event_topic(CommitScore, &mut topic)?;
    assert_eq!(&topic[..n], b"PAXAI/v1/CommitScore");
    assert_eq!(
        decode_event_frame(&topic[..n], &hex(COMMIT_EVENT_BYTES))?.0,
        CommitScore
    );
    let mut bad = input.clone();
    bad[218..220].copy_from_slice(&33_u16.to_be_bytes());
    assert!(decode_reveal_event(&bad).is_err());
    let mut bad = input.clone();
    bad.push(0);
    assert!(decode_reveal_event(&bad).is_err());
    assert!(decode_event_frame(b"PAXAI/v1/UNKNOWN", &input).is_err());
    assert!(decode_event_frame(&[b'A'; 65], &input).is_err());
    assert!(decode_event_frame(b"PAXAI/v1/RevealScore", &vec![0; 2049]).is_err());
    assert!(encode_event_frame(READ_HEADER, &common()?, &[], &mut out).is_err());
    assert!(encode_event_frame(CommitScore, &common()?, &[0; 72], &mut out).is_err());
    let suffix = vec![1; 1926];
    assert_eq!(
        encode_event_frame(OPEN_EPOCH, &common()?, &suffix, &mut out)?,
        2048
    );
    assert!(encode_event_frame(OPEN_EPOCH, &common()?, &vec![1; 1927], &mut out).is_err());
    Ok(())
}

#[test]
fn independent_state_frame_hash_all_section_caps_and_refusals() -> Checked {
    let state = StateFrame {
        revision: 1,
        sections: [&[]; 6],
    };
    let mut out = vec![0; 196_609];
    assert_eq!(state.encoded_len()?, 64);
    assert_eq!(encode_state(&state, &mut out)?, 64);
    assert_eq!(&out[..64], hex(STATE_BYTES).as_slice());
    assert_eq!(state_digest(&out[..64])?.bytes(), fixed32(STATE_DIGEST)?);
    let input = hex(STATE_BYTES);
    assert_eq!(decode_state(&input)?, state);
    for n in 0..64 {
        assert!(decode_state(&input[..n]).is_err());
    }
    for offset in [0, 7, 15, 17, 19, 23, 25, 33, 41, 49, 57] {
        let mut bad = input.clone();
        bad[offset] ^= 1;
        assert!(decode_state(&bad).is_err());
    }
    let mut trailing = input.clone();
    trailing.push(0);
    assert!(decode_state(&trailing).is_err());
    assert!(state_digest(&trailing).is_err());
    assert!(StateFrame {
        revision: 0,
        ..state
    }
    .encoded_len()
    .is_err());
    let maxima = [16376, 24568, 24568, 81912, 24568, 24552];
    let payloads: Vec<Vec<u8>> = maxima.iter().map(|&n| vec![1; n]).collect();
    let full = StateFrame {
        revision: 1,
        sections: [
            &payloads[0],
            &payloads[1],
            &payloads[2],
            &payloads[3],
            &payloads[4],
            &payloads[5],
        ],
    };
    assert_eq!(full.encoded_len()?, 196_608);
    assert_eq!(encode_state(&full, &mut out)?, 196_608);
    assert_eq!(decode_state(&out[..196_608])?, full);
    for i in 0..6 {
        let excess = vec![1; maxima[i] + 1];
        let mut sections = [&[][..]; 6];
        sections[i] = &excess;
        let oversized = StateFrame {
            revision: 1,
            sections,
        };
        assert_eq!(oversized.encoded_len(), Err(CAPACITY));
        let mut scratch = vec![0x5a; 196_609];
        assert_eq!(encode_state(&oversized, &mut scratch), Err(CAPACITY));
        assert!(scratch.iter().all(|&b| b == 0x5a));
        let mut declared = input.clone();
        let start = 16 + i * 8;
        declared[start + 4..start + 8]
            .copy_from_slice(&u32::try_from(maxima[i] + 1)?.to_be_bytes());
        assert_eq!(decode_state(&declared).err(), Some(CAPACITY));
    }
    assert_eq!(decode_state(&vec![0; 196_609]).err(), Some(CAPACITY));
    let mut short = [0x5a; 63];
    assert_eq!(encode_state(&state, &mut short), Err(CAPACITY));
    assert_eq!(short, [0x5a; 63]);
    Ok(())
}

#[test]
fn read_header_optional_epoch_zero_and_exact_chunk_binding() -> Checked {
    let digest = StateDigest::new(fixed32(STATE_DIGEST)?)?;
    read_header_bindings(digest)?;
    let request = ChunkRequest {
        revision: 0,
        digest: Presence::Absent,
        offset: 0,
        requested: 8192,
    };
    let mut rb = [0; 46];
    assert_eq!(encode_chunk_request(&request, &mut rb)?, 46);
    assert_eq!(&rb[..44], &[0; 44]);
    assert_eq!(&rb[44..], &[0x20, 0]);
    assert_eq!(decode_chunk_request(&rb)?, request);
    let data = hex(STATE_BYTES);
    let response = ChunkResponse {
        revision: 1,
        digest,
        total_bytes: 64,
        offset: 0,
        bytes: &data,
    };
    let mut out = vec![0; 8244];
    let bound = chunk_response_binding(request, response, digest, &data, &mut out)?;
    chunk_refusals_and_maximum(request, response, bound, digest, &mut out)?;
    Ok(())
}

fn read_header_bindings(digest: StateDigest) -> Checked {
    let header = ReadHeader {
        revision: 1,
        digest,
        total_bytes: 64,
        chain: ChainDomain::new([1; 32])?,
        program: ProgramId::new([2; 32])?,
        market: MarketId::new([3; 32])?,
        epoch: Presence::Absent,
        config: Version::new(1)?,
        roster: Presence::Absent,
    };
    let mut bytes = [0; 192];
    assert_eq!(encode_read_header(&header, &mut bytes)?, 152);
    assert_eq!(decode_read_header(&bytes[..152])?, header);
    assert_eq!(bytes[142], 0);
    assert_eq!(bytes[151], 0);
    let opened = ReadHeader {
        epoch: Presence::Present(0),
        roster: Presence::Present(RosterDigest::new([4; 32])?),
        ..header
    };
    assert_eq!(encode_read_header(&opened, &mut bytes)?, 192);
    assert_eq!(decode_read_header(&bytes)?, opened);
    assert_eq!(bytes[142], 1);
    assert_eq!(&bytes[143..151], &[0; 8]);
    assert_eq!(bytes[159], 1);
    for n in 0..192 {
        assert!(decode_read_header(&bytes[..n]).is_err());
    }
    let mut invalid = bytes;
    invalid[142] = 2;
    assert!(decode_read_header(&invalid).is_err());
    assert!(encode_read_header(
        &ReadHeader {
            roster: Presence::Absent,
            ..opened
        },
        &mut bytes
    )
    .is_err());
    assert!(encode_read_header(
        &ReadHeader {
            revision: 0,
            ..header
        },
        &mut bytes
    )
    .is_err());
    assert!(encode_read_header(
        &ReadHeader {
            total_bytes: 196_609,
            ..header
        },
        &mut bytes
    )
    .is_err());
    Ok(())
}

fn chunk_response_binding(
    request: ChunkRequest,
    response: ChunkResponse<'_>,
    digest: StateDigest,
    data: &[u8],
    out: &mut [u8],
) -> Checked<ChunkRequest> {
    assert_eq!(encode_chunk_response(&response, out)?, 116);
    assert_eq!(&out[..2], &[0, 1]);
    assert_eq!(&out[42..46], &64_u32.to_be_bytes());
    assert_eq!(&out[50..52], &64_u16.to_be_bytes());
    assert_eq!(&out[52..116], data);
    assert_eq!(decode_chunk_response(&out[..116])?, response);
    assert_eq!(check_chunk_response(&request, &response), Ok(()));
    let bound = ChunkRequest {
        revision: 1,
        digest: Presence::Present(digest),
        ..request
    };
    assert_eq!(check_chunk_response(&bound, &response), Ok(()));
    assert_eq!(
        check_chunk_response(
            &ChunkRequest {
                revision: 2,
                ..bound
            },
            &response
        ),
        Err(CONFLICT)
    );
    assert_eq!(
        check_chunk_response(
            &ChunkRequest {
                digest: Presence::Present(StateDigest::new([1; 32])?),
                ..bound
            },
            &response
        ),
        Err(CONFLICT)
    );
    Ok(bound)
}

fn chunk_refusals_and_maximum(
    request: ChunkRequest,
    response: ChunkResponse<'_>,
    bound: ChunkRequest,
    digest: StateDigest,
    out: &mut [u8],
) -> Checked {
    for invalid in [
        ChunkRequest {
            revision: 1,
            ..request
        },
        ChunkRequest {
            digest: Presence::Present(digest),
            ..request
        },
        ChunkRequest {
            offset: 1,
            ..request
        },
        ChunkRequest {
            offset: 196_608,
            ..request
        },
        ChunkRequest {
            requested: 0,
            ..request
        },
        ChunkRequest {
            requested: 8193,
            ..request
        },
    ] {
        assert!(invalid.validate().is_err());
    }
    assert!(check_chunk_response(
        &ChunkRequest {
            requested: 63,
            ..request
        },
        &response
    )
    .is_err());
    assert!(ChunkResponse {
        bytes: &[],
        ..response
    }
    .validate()
    .is_err());
    assert!(ChunkResponse {
        total_bytes: 63,
        ..response
    }
    .validate()
    .is_err());
    assert!(ChunkResponse {
        offset: 8192,
        ..response
    }
    .validate()
    .is_err());
    for n in 0..116 {
        assert!(decode_chunk_response(&out[..n]).is_err());
    }
    let mut trailing = out[..116].to_vec();
    trailing.push(0);
    assert!(decode_chunk_response(&trailing).is_err());
    let page = vec![1; 8192];
    let maximum = ChunkResponse {
        total_bytes: 196_608,
        offset: 188_416,
        bytes: &page,
        ..response
    };
    assert_eq!(encode_chunk_response(&maximum, out)?, 8244);
    assert_eq!(
        check_chunk_response(
            &ChunkRequest {
                offset: 188_416,
                ..bound
            },
            &maximum
        ),
        Ok(())
    );
    Ok(())
}

#[test]
fn replay_exact_retry_conflicts_gaps_and_checked_epoch_windows() -> Checked {
    let request = RequestId::new([1; 32])?;
    let digest = RequestDigest::new([2; 32])?;
    let result = ResultDigest::new([3; 32])?;
    let empty = RoleReplay {
        sequence: 0,
        request: Presence::Absent,
        digest: Presence::Absent,
        result: Presence::Absent,
    };
    assert_eq!(empty.assess(1, request, digest), Ok(ReplayDecision::New));
    assert_eq!(empty.assess(0, request, digest), Err(SEQUENCE_CONSUMED));
    assert_eq!(empty.assess(2, request, digest), Err(SEQUENCE_GAP));
    let replay = RoleReplay {
        sequence: 1,
        request: Presence::Present(request),
        digest: Presence::Present(digest),
        result: Presence::Present(result),
    };
    assert_eq!(
        replay.assess(1, request, digest),
        Ok(ReplayDecision::AlreadyApplied(result))
    );
    assert_eq!(replay.assess(2, request, digest), Ok(ReplayDecision::New));
    assert_eq!(replay.assess(3, request, digest), Err(SEQUENCE_GAP));
    assert_eq!(replay.assess(0, request, digest), Err(SEQUENCE_CONSUMED));
    assert_eq!(
        replay.assess(1, RequestId::new([4; 32])?, digest),
        Err(REPLAY_CONFLICT)
    );
    assert_eq!(
        replay.assess(1, request, RequestDigest::new([4; 32])?),
        Err(REPLAY_CONFLICT)
    );
    assert_eq!(
        RoleReplay {
            result: Presence::Absent,
            ..replay
        }
        .assess(2, request, digest),
        Err(NON_CANONICAL)
    );
    assert_eq!(
        RoleReplay {
            request: Presence::Present(request),
            ..empty
        }
        .assess(1, request, digest),
        Err(NON_CANONICAL)
    );
    let terminal = RoleReplay {
        sequence: u64::MAX,
        ..replay
    };
    assert_eq!(
        terminal.assess(u64::MAX, request, digest),
        Ok(ReplayDecision::AlreadyApplied(result))
    );
    assert_eq!(terminal.assess(1, request, digest), Err(SEQUENCE_CONSUMED));
    let windows = EpochWindows::new(0, 0)?;
    assert_eq!(
        windows,
        EpochWindows {
            start: 0,
            commit: 64,
            reveal: 80,
            settlement: 96,
            end: 128
        }
    );
    for (height, phase) in [
        (0, EpochPhase::Work),
        (63, EpochPhase::Work),
        (64, EpochPhase::Commit),
        (79, EpochPhase::Commit),
        (80, EpochPhase::Reveal),
        (95, EpochPhase::Reveal),
        (96, EpochPhase::Settlement),
        (127, EpochPhase::Settlement),
        (128, EpochPhase::After),
    ] {
        assert_eq!(windows.phase(height), phase);
    }
    assert_eq!(EpochWindows::new(5, 1)?.start, 133);
    assert_eq!(EpochWindows::new(5, 1)?.phase(132), EpochPhase::Before);
    assert_eq!(EpochWindows::new(0, u64::MAX), Err(ARITHMETIC));
    assert_eq!(EpochWindows::new(u64::MAX, 0), Err(ARITHMETIC));
    Ok(())
}

#[test]
fn pre_epoch_refund_market_ledger_envelope_reaches_handler_boundary() -> Checked {
    let fixture = hex(NATIVE_BYTES);
    let base = decode_envelope(&fixture)?.envelope;
    // Exact RefundFree payload: cursor:u128 || amount:u128 || recipient32.
    let mut payload = [0; 64];
    payload[16..32].copy_from_slice(&20_u128.to_be_bytes());
    payload[32..64].fill(9);
    let refund = Envelope {
        operation: REFUND_FREE,
        config: 1,
        payload: &payload,
        ..base
    };
    assert_eq!(refund.epoch, 0);
    assert_eq!(refund.roster, Presence::Absent);
    assert_eq!(refund.validate(), Ok(()));
    let mut bytes = [0; 512];
    let n = encode_envelope(&refund, &mut bytes)?;
    assert_eq!(n, ENVELOPE_PREFIX_BYTES + 64 + 1);
    assert_eq!(&bytes[8..10], &0x0604_u16.to_be_bytes());
    assert_eq!(&bytes[154..186], &[0; 32]);
    let decoded = decode_envelope(&bytes[..n])?;
    assert_eq!(decoded.envelope, refund);
    assert_eq!(decoded.envelope.validate(), Ok(()));
    // Admission exposes the envelope for handler checks; it supplies no ledger authority.
    assert_eq!(
        decoded
            .envelope
            .check_domain(base.chain, base.program, base.market),
        Ok(())
    );
    assert_eq!(
        decoded
            .envelope
            .check_domain(base.chain, base.program, MarketId::new([7; 32])?),
        Err(WRONG_MARKET)
    );
    assert_eq!(
        compare_native_principal(&decoded.envelope, base.actor),
        Ok(())
    );
    assert_eq!(
        compare_native_principal(&decoded.envelope, PrincipalId::new([7; 32])?),
        Err(UNAUTHORIZED)
    );
    assert_eq!(decoded.envelope.check_expiry(99), Ok(()));
    assert_eq!(decoded.envelope.check_expiry(100), Err(EXPIRED));
    let mut roundtrip = [0; 512];
    assert_eq!(encode_envelope(&decoded.envelope, &mut roundtrip)?, n);
    assert_eq!(&roundtrip[..n], &bytes[..n]);

    let nonzero_epoch = Envelope { epoch: 1, ..refund };
    assert_eq!(nonzero_epoch.validate(), Err(WRONG_ROSTER));
    assert_eq!(
        encode_envelope(&nonzero_epoch, &mut roundtrip),
        Err(WRONG_ROSTER)
    );
    let mut nonzero_wire = bytes[..n].to_vec();
    nonzero_wire[138..146].copy_from_slice(&1_u64.to_be_bytes());
    assert_eq!(decode_envelope(&nonzero_wire).err(), Some(WRONG_ROSTER));

    // Existing canonical and authentication refusals still apply to the newly admitted selector.
    for invalid in [
        Envelope {
            config: 0,
            ..refund
        },
        Envelope {
            sequence: 1,
            ..refund
        },
        Envelope {
            expiry: 0,
            ..refund
        },
        Envelope {
            payload: &payload[..63],
            ..refund
        },
    ] {
        assert!(invalid.validate().is_err());
        assert!(encode_envelope(&invalid, &mut roundtrip).is_err());
    }
    let delegate = Envelope {
        authentication: Authentication::Delegate {
            key: PublicKey32([10; 32]),
            signature: Signature64([11; 64]),
        },
        ..refund
    };
    assert_eq!(delegate.validate(), Err(UNAUTHORIZED));
    assert_eq!(
        encode_envelope(&delegate, &mut roundtrip),
        Err(UNAUTHORIZED)
    );
    let mut delegate_wire = bytes[..n - 1].to_vec();
    delegate_wire.push(1);
    delegate_wire.extend_from_slice(&[10; 32]);
    delegate_wire.extend_from_slice(&[11; 64]);
    assert_eq!(decode_envelope(&delegate_wire).err(), Some(UNAUTHORIZED));
    let mut trailing = bytes[..n].to_vec();
    trailing.push(0);
    assert_eq!(decode_envelope(&trailing).err(), Some(NON_CANONICAL));
    Ok(())
}

#[test]
fn epoch_bound_reward_operations_require_roster_even_at_epoch_zero() -> Checked {
    let fixture = hex(NATIVE_BYTES);
    let base = decode_envelope(&fixture)?.envelope;
    let claim_payload = [1; 80];
    for (operation, payload) in [
        (CLAIM, &claim_payload[..]),
        (EXPIRE_EPOCH_CLAIMS, &[][..]),
        (PRUNE_EPOCH, &[][..]),
    ] {
        for epoch in [0, 1] {
            let absent = Envelope {
                operation,
                epoch,
                config: 1,
                roster: Presence::Absent,
                payload,
                ..base
            };
            assert_eq!(absent.validate(), Err(WRONG_ROSTER));
            let mut bytes = [0; 512];
            assert_eq!(encode_envelope(&absent, &mut bytes), Err(WRONG_ROSTER));
            // Produce real canonical bytes with a roster, then remove only that binding.
            let bound = Envelope {
                roster: Presence::Present(RosterDigest::new([4; 32])?),
                ..absent
            };
            assert_eq!(bound.validate(), Ok(()));
            let n = encode_envelope(&bound, &mut bytes)?;
            assert_eq!(decode_envelope(&bytes[..n])?.envelope, bound);
            bytes[154..186].fill(0);
            assert_eq!(decode_envelope(&bytes[..n]).err(), Some(WRONG_ROSTER));
        }
    }
    Ok(())
}

#[test]
fn first_open_epoch_zero_absent_roster_envelope_reaches_handler_boundary() -> Checked {
    let fixture = hex(NATIVE_BYTES);
    let base = decode_envelope(&fixture)?.envelope;
    // The pinned check_open_binding accepts this binding only before any epoch is opened.
    // Codec admission supplies no lifecycle, readiness, or caller authority.
    let first_open = Envelope {
        operation: OPEN_EPOCH,
        config: 1,
        ..base
    };
    assert_eq!(first_open.epoch, 0);
    assert_eq!(first_open.roster, Presence::Absent);
    assert_eq!(first_open.sequence, 0);
    assert_eq!(first_open.authentication, Authentication::Native);
    assert_eq!(first_open.validate(), Ok(()));
    let mut bytes = [0; 512];
    let n = encode_envelope(&first_open, &mut bytes)?;
    assert_eq!(n, ENVELOPE_PREFIX_BYTES + 1);
    assert_eq!(&bytes[8..10], &0x0111_u16.to_be_bytes());
    assert_eq!(&bytes[138..146], &0_u64.to_be_bytes());
    assert_eq!(&bytes[154..186], &[0; 32]);
    let decoded = decode_envelope(&bytes[..n])?;
    assert_eq!(decoded.envelope, first_open);
    assert_eq!(decoded.envelope.validate(), Ok(()));
    assert_eq!(
        decoded
            .envelope
            .check_domain(base.chain, base.program, base.market),
        Ok(())
    );
    assert_eq!(
        decoded
            .envelope
            .check_domain(base.chain, base.program, MarketId::new([7; 32])?),
        Err(WRONG_MARKET)
    );
    assert_eq!(
        compare_native_principal(&decoded.envelope, base.actor),
        Ok(())
    );
    assert_eq!(
        compare_native_principal(&decoded.envelope, PrincipalId::new([7; 32])?),
        Err(UNAUTHORIZED)
    );
    assert_eq!(decoded.envelope.check_expiry(100), Err(EXPIRED));
    let mut roundtrip = [0; 512];
    assert_eq!(encode_envelope(&decoded.envelope, &mut roundtrip)?, n);
    assert_eq!(&roundtrip[..n], &bytes[..n]);

    for invalid in [
        Envelope {
            config: 0,
            ..first_open
        },
        Envelope {
            sequence: 1,
            ..first_open
        },
        Envelope {
            expiry: 0,
            ..first_open
        },
    ] {
        assert_eq!(invalid.validate(), Err(NON_CANONICAL));
        assert_eq!(
            encode_envelope(&invalid, &mut roundtrip),
            Err(NON_CANONICAL)
        );
    }
    let delegate = Envelope {
        authentication: Authentication::Delegate {
            key: PublicKey32([10; 32]),
            signature: Signature64([11; 64]),
        },
        ..first_open
    };
    assert_eq!(delegate.validate(), Err(UNAUTHORIZED));
    assert_eq!(
        encode_envelope(&delegate, &mut roundtrip),
        Err(UNAUTHORIZED)
    );
    let mut delegate_wire = bytes[..n - 1].to_vec();
    delegate_wire.push(1);
    delegate_wire.extend_from_slice(&[10; 32]);
    delegate_wire.extend_from_slice(&[11; 64]);
    assert_eq!(decode_envelope(&delegate_wire).err(), Some(UNAUTHORIZED));
    Ok(())
}

#[test]
fn open_epoch_absent_roster_refuses_nonzero_epochs() -> Checked {
    let fixture = hex(NATIVE_BYTES);
    let base = decode_envelope(&fixture)?.envelope;
    let first_open = Envelope {
        operation: OPEN_EPOCH,
        config: 1,
        ..base
    };
    let mut bytes = [0; 512];
    let n = encode_envelope(&first_open, &mut bytes)?;
    for epoch in [1, 2, u64::MAX] {
        let invalid = Envelope {
            epoch,
            ..first_open
        };
        assert_eq!(invalid.validate(), Err(WRONG_ROSTER));
        let mut output = [0; 512];
        assert_eq!(encode_envelope(&invalid, &mut output), Err(WRONG_ROSTER));
        bytes[138..146].copy_from_slice(&epoch.to_be_bytes());
        assert_eq!(decode_envelope(&bytes[..n]).err(), Some(WRONG_ROSTER));
    }
    Ok(())
}

#[test]
fn open_epoch_present_roster_retains_ordinary_envelope_binding() -> Checked {
    let fixture = hex(NATIVE_BYTES);
    let base = decode_envelope(&fixture)?.envelope;
    let roster = RosterDigest::new([4; 32])?;
    // The handler must compare these fields to the actual current frozen binding.
    for epoch in [0, 1, u64::MAX] {
        let bound = Envelope {
            operation: OPEN_EPOCH,
            epoch,
            config: 1,
            roster: Presence::Present(roster),
            ..base
        };
        assert_eq!(bound.validate(), Ok(()));
        let mut bytes = [0; 512];
        let n = encode_envelope(&bound, &mut bytes)?;
        assert_eq!(&bytes[154..186], roster.as_bytes());
        let decoded = decode_envelope(&bytes[..n])?;
        assert_eq!(decoded.envelope, bound);
        assert_eq!(decoded.envelope.validate(), Ok(()));
        let mut roundtrip = [0; 512];
        assert_eq!(encode_envelope(&decoded.envelope, &mut roundtrip)?, n);
        assert_eq!(&roundtrip[..n], &bytes[..n]);
    }
    Ok(())
}
