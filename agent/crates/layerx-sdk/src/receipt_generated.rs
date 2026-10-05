//! Code generated from platform/sdk/generators/receipt.kvx. DO NOT EDIT.

pub const PROGRAMS_MODULE_ID: u16 = 9;
pub const PROGRAM_OUTCOME_TAGS: [u32; 4] = [0x5052_4731, 0x5052_4732, 0x5052_4733, 0x5052_4734];

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ReceiptFailureCode {
    Decode,
    CanonicalEncoding,
    ReceiptShape,
    MissingSignature,
    ProtocolVersion,
    ResultCode,
    Operation,
    ActivityId,
    GlobalSequence,
    ModuleId,
    ModuleVersion,
    Timestamp,
    BatchId,
    Asset,
    PreviousStateRoot,
    ResultingStateRoot,
    DebitBalance,
    CreditBalance,
    ProgramOutcome,
    SequencerSignature,
}

impl ReceiptFailureCode {
    #[must_use]
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::Decode => "decode",
            Self::CanonicalEncoding => "canonical-encoding",
            Self::ReceiptShape => "receipt-shape",
            Self::MissingSignature => "missing-signature",
            Self::ProtocolVersion => "protocol-version",
            Self::ResultCode => "result-code",
            Self::Operation => "operation",
            Self::ActivityId => "activity-id",
            Self::GlobalSequence => "global-sequence",
            Self::ModuleId => "module-id",
            Self::ModuleVersion => "module-version",
            Self::Timestamp => "timestamp",
            Self::BatchId => "batch-id",
            Self::Asset => "asset",
            Self::PreviousStateRoot => "previous-state-root",
            Self::ResultingStateRoot => "resulting-state-root",
            Self::DebitBalance => "debit-balance",
            Self::CreditBalance => "credit-balance",
            Self::ProgramOutcome => "program-outcome",
            Self::SequencerSignature => "sequencer-signature",
        }
    }
}

pub const REQUIRED_NONZERO_CHECKS: &[ReceiptFailureCode] = &[
    ReceiptFailureCode::GlobalSequence,
    ReceiptFailureCode::ModuleId,
    ReceiptFailureCode::ModuleVersion,
    ReceiptFailureCode::Timestamp,
    ReceiptFailureCode::ActivityId,
    ReceiptFailureCode::ResultingStateRoot,
];

pub const PROGRAM_ABI_V1: u16 = 1;
pub const PROGRAM_ABI_V2: u16 = 2;
pub const PROGRAM_ABI_V3: u16 = 3;
pub const PROGRAM_ABI_V4: u16 = 4;
#[must_use]
pub const fn supports_program_guest_abi(version: u16) -> bool {
    matches!(version, PROGRAM_ABI_V1 | PROGRAM_ABI_V2 | PROGRAM_ABI_V3 | PROGRAM_ABI_V4)
}
