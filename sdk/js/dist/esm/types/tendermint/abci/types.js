export var CheckTxType;
(function (CheckTxType) {
    CheckTxType[CheckTxType["NEW"] = 0] = "NEW";
    CheckTxType[CheckTxType["RECHECK"] = 1] = "RECHECK";
    CheckTxType[CheckTxType["UNRECOGNIZED"] = -1] = "UNRECOGNIZED";
})(CheckTxType || (CheckTxType = {}));
export var MisbehaviorType;
(function (MisbehaviorType) {
    MisbehaviorType[MisbehaviorType["UNKNOWN"] = 0] = "UNKNOWN";
    MisbehaviorType[MisbehaviorType["DUPLICATE_VOTE"] = 1] = "DUPLICATE_VOTE";
    MisbehaviorType[MisbehaviorType["LIGHT_CLIENT_ATTACK"] = 2] = "LIGHT_CLIENT_ATTACK";
    MisbehaviorType[MisbehaviorType["UNRECOGNIZED"] = -1] = "UNRECOGNIZED";
})(MisbehaviorType || (MisbehaviorType = {}));
export var ResponseOfferSnapshotResult;
(function (ResponseOfferSnapshotResult) {
    /** UNKNOWN - Unknown result, abort all snapshot restoration */
    ResponseOfferSnapshotResult[ResponseOfferSnapshotResult["UNKNOWN"] = 0] = "UNKNOWN";
    /** ACCEPT - Snapshot accepted, apply chunks */
    ResponseOfferSnapshotResult[ResponseOfferSnapshotResult["ACCEPT"] = 1] = "ACCEPT";
    /** ABORT - Abort all snapshot restoration */
    ResponseOfferSnapshotResult[ResponseOfferSnapshotResult["ABORT"] = 2] = "ABORT";
    /** REJECT - Reject this specific snapshot, try others */
    ResponseOfferSnapshotResult[ResponseOfferSnapshotResult["REJECT"] = 3] = "REJECT";
    /** REJECT_FORMAT - Reject all snapshots of this format, try others */
    ResponseOfferSnapshotResult[ResponseOfferSnapshotResult["REJECT_FORMAT"] = 4] = "REJECT_FORMAT";
    /** REJECT_SENDER - Reject all snapshots from the sender(s), try others */
    ResponseOfferSnapshotResult[ResponseOfferSnapshotResult["REJECT_SENDER"] = 5] = "REJECT_SENDER";
    ResponseOfferSnapshotResult[ResponseOfferSnapshotResult["UNRECOGNIZED"] = -1] = "UNRECOGNIZED";
})(ResponseOfferSnapshotResult || (ResponseOfferSnapshotResult = {}));
export var ResponseApplySnapshotChunkResult;
(function (ResponseApplySnapshotChunkResult) {
    /** UNKNOWN - Unknown result, abort all snapshot restoration */
    ResponseApplySnapshotChunkResult[ResponseApplySnapshotChunkResult["UNKNOWN"] = 0] = "UNKNOWN";
    /** ACCEPT - Chunk successfully accepted */
    ResponseApplySnapshotChunkResult[ResponseApplySnapshotChunkResult["ACCEPT"] = 1] = "ACCEPT";
    /** ABORT - Abort all snapshot restoration */
    ResponseApplySnapshotChunkResult[ResponseApplySnapshotChunkResult["ABORT"] = 2] = "ABORT";
    /** RETRY - Retry chunk (combine with refetch and reject) */
    ResponseApplySnapshotChunkResult[ResponseApplySnapshotChunkResult["RETRY"] = 3] = "RETRY";
    /** RETRY_SNAPSHOT - Retry snapshot (combine with refetch and reject) */
    ResponseApplySnapshotChunkResult[ResponseApplySnapshotChunkResult["RETRY_SNAPSHOT"] = 4] = "RETRY_SNAPSHOT";
    /** REJECT_SNAPSHOT - Reject this snapshot, try others */
    ResponseApplySnapshotChunkResult[ResponseApplySnapshotChunkResult["REJECT_SNAPSHOT"] = 5] = "REJECT_SNAPSHOT";
    ResponseApplySnapshotChunkResult[ResponseApplySnapshotChunkResult["UNRECOGNIZED"] = -1] = "UNRECOGNIZED";
})(ResponseApplySnapshotChunkResult || (ResponseApplySnapshotChunkResult = {}));
export var ResponseProcessProposalProposalStatus;
(function (ResponseProcessProposalProposalStatus) {
    ResponseProcessProposalProposalStatus[ResponseProcessProposalProposalStatus["UNKNOWN"] = 0] = "UNKNOWN";
    ResponseProcessProposalProposalStatus[ResponseProcessProposalProposalStatus["ACCEPT"] = 1] = "ACCEPT";
    ResponseProcessProposalProposalStatus[ResponseProcessProposalProposalStatus["REJECT"] = 2] = "REJECT";
    ResponseProcessProposalProposalStatus[ResponseProcessProposalProposalStatus["UNRECOGNIZED"] = -1] = "UNRECOGNIZED";
})(ResponseProcessProposalProposalStatus || (ResponseProcessProposalProposalStatus = {}));
export var ResponseVerifyVoteExtensionVerifyStatus;
(function (ResponseVerifyVoteExtensionVerifyStatus) {
    ResponseVerifyVoteExtensionVerifyStatus[ResponseVerifyVoteExtensionVerifyStatus["UNKNOWN"] = 0] = "UNKNOWN";
    ResponseVerifyVoteExtensionVerifyStatus[ResponseVerifyVoteExtensionVerifyStatus["ACCEPT"] = 1] = "ACCEPT";
    ResponseVerifyVoteExtensionVerifyStatus[ResponseVerifyVoteExtensionVerifyStatus["REJECT"] = 2] = "REJECT";
    ResponseVerifyVoteExtensionVerifyStatus[ResponseVerifyVoteExtensionVerifyStatus["UNRECOGNIZED"] = -1] = "UNRECOGNIZED";
})(ResponseVerifyVoteExtensionVerifyStatus || (ResponseVerifyVoteExtensionVerifyStatus = {}));
export var TxRecordTxAction;
(function (TxRecordTxAction) {
    /** UNKNOWN - Unknown action */
    TxRecordTxAction[TxRecordTxAction["UNKNOWN"] = 0] = "UNKNOWN";
    /** UNMODIFIED - The Application did not modify this transaction. */
    TxRecordTxAction[TxRecordTxAction["UNMODIFIED"] = 1] = "UNMODIFIED";
    /** ADDED - The Application added this transaction. */
    TxRecordTxAction[TxRecordTxAction["ADDED"] = 2] = "ADDED";
    /** REMOVED - The Application wants this transaction removed from the proposal and the mempool. */
    TxRecordTxAction[TxRecordTxAction["REMOVED"] = 3] = "REMOVED";
    TxRecordTxAction[TxRecordTxAction["UNRECOGNIZED"] = -1] = "UNRECOGNIZED";
})(TxRecordTxAction || (TxRecordTxAction = {}));
