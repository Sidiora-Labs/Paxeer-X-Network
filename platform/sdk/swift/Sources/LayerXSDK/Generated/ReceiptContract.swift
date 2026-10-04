// Code generated from platform/sdk/generators/receipt.kvx. DO NOT EDIT.

let programsModuleID: UInt16 = 9
let programOutcomeV1: UInt32 = 0x50524731
let programOutcomeV2: UInt32 = 0x50524732
let programOutcomeV3: UInt32 = 0x50524733

public enum ReceiptCheck: String, Sendable, CaseIterable {
    case decode = "decode"
    case canonicalEncoding = "canonical-encoding"
    case receiptShape = "receipt-shape"
    case missingSignature = "missing-signature"
    case protocolVersion = "protocol-version"
    case resultCode = "result-code"
    case operation = "operation"
    case activityId = "activity-id"
    case globalSequence = "global-sequence"
    case moduleId = "module-id"
    case moduleVersion = "module-version"
    case timestamp = "timestamp"
    case batchId = "batch-id"
    case asset = "asset"
    case previousStateRoot = "previous-state-root"
    case resultingStateRoot = "resulting-state-root"
    case debitBalance = "debit-balance"
    case creditBalance = "credit-balance"
    case programOutcome = "program-outcome"
    case sequencerSignature = "sequencer-signature"
}

let requiredNonzeroChecks: [ReceiptCheck] = [
    .globalSequence,
    .moduleId,
    .moduleVersion,
    .timestamp,
    .activityId,
    .resultingStateRoot,
]

let programAbiV1: UInt16 = 1
let programAbiV2: UInt16 = 2
let programAbiV3: UInt16 = 3
let programAbiV4: UInt16 = 4
func supportsProgramGuestAbi(_ version: UInt16) -> Bool {
    switch version {
    case programAbiV1, programAbiV2, programAbiV3, programAbiV4: return true
    default: return false
    }
}
