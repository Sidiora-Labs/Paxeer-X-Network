import type { PublicKey } from "../crypto/keys";
import type { ProofOps } from "../crypto/proof";
import type { ConsensusParams } from "../types/params";
export declare enum CheckTxType {
    NEW = 0,
    RECHECK = 1,
    UNRECOGNIZED = -1
}
export declare enum MisbehaviorType {
    UNKNOWN = 0,
    DUPLICATE_VOTE = 1,
    LIGHT_CLIENT_ATTACK = 2,
    UNRECOGNIZED = -1
}
export interface Request {
    echo?: RequestEcho;
    flush?: RequestFlush;
    info?: RequestInfo;
    init_chain?: RequestInitChain;
    query?: RequestQuery;
    check_tx?: RequestCheckTx;
    commit?: RequestCommit;
    list_snapshots?: RequestListSnapshots;
    offer_snapshot?: RequestOfferSnapshot;
    load_snapshot_chunk?: RequestLoadSnapshotChunk;
    apply_snapshot_chunk?: RequestApplySnapshotChunk;
    prepare_proposal?: RequestPrepareProposal;
    process_proposal?: RequestProcessProposal;
    extend_vote?: RequestExtendVote;
    verify_vote_extension?: RequestVerifyVoteExtension;
    finalize_block?: RequestFinalizeBlock;
}
export interface RequestEcho {
    message: string;
}
export type RequestFlush = {};
export interface RequestInfo {
    version: string;
    block_version: number;
    p2p_version: number;
    abci_version: string;
}
export interface RequestInitChain {
    time?: Date;
    chain_id: string;
    consensus_params?: ConsensusParams;
    validators: ValidatorUpdate[];
    app_state_bytes: Uint8Array;
    initial_height: number;
}
export interface RequestQuery {
    data: Uint8Array;
    path: string;
    height: number;
    prove: boolean;
}
export interface RequestCheckTx {
    tx: Uint8Array;
    type: CheckTxType;
}
export type RequestCommit = {};
export type RequestListSnapshots = {};
export interface RequestOfferSnapshot {
    /** snapshot offered by peers */
    snapshot?: Snapshot;
    /** light client-verified app hash for snapshot height */
    app_hash: Uint8Array;
}
export interface RequestLoadSnapshotChunk {
    height: number;
    format: number;
    chunk: number;
}
export interface RequestApplySnapshotChunk {
    index: number;
    chunk: Uint8Array;
    sender: string;
}
export interface RequestPrepareProposal {
    /** the modified transactions cannot exceed this size. */
    max_tx_bytes: number;
    /**
     * txs is an array of transactions that will be included in a block,
     * sent to the app for possible modifications.
     */
    txs: Uint8Array[];
    local_last_commit?: ExtendedCommitInfo;
    byzantine_validators: Misbehavior[];
    height: number;
    time?: Date;
    next_validators_hash: Uint8Array;
    /** address of the public key of the validator proposing the block. */
    proposer_address: Uint8Array;
}
export interface RequestProcessProposal {
    txs: Uint8Array[];
    proposed_last_commit?: CommitInfo;
    byzantine_validators: Misbehavior[];
    /** hash is the merkle root hash of the fields of the proposed block. */
    hash: Uint8Array;
    height: number;
    time?: Date;
    next_validators_hash: Uint8Array;
    /** address of the public key of the original proposer of the block. */
    proposer_address: Uint8Array;
}
export interface RequestExtendVote {
    hash: Uint8Array;
    height: number;
}
export interface RequestVerifyVoteExtension {
    hash: Uint8Array;
    validator_address: Uint8Array;
    height: number;
    vote_extension: Uint8Array;
}
export interface RequestFinalizeBlock {
    txs: Uint8Array[];
    decided_last_commit?: CommitInfo;
    byzantine_validators: Misbehavior[];
    /** hash is the merkle root hash of the fields of the proposed block. */
    hash: Uint8Array;
    height: number;
    time?: Date;
    next_validators_hash: Uint8Array;
    /** proposer_address is the address of the public key of the original proposer of the block. */
    proposer_address: Uint8Array;
}
export interface Response {
    exception?: ResponseException;
    echo?: ResponseEcho;
    flush?: ResponseFlush;
    info?: ResponseInfo;
    init_chain?: ResponseInitChain;
    query?: ResponseQuery;
    check_tx?: ResponseCheckTx;
    commit?: ResponseCommit;
    list_snapshots?: ResponseListSnapshots;
    offer_snapshot?: ResponseOfferSnapshot;
    load_snapshot_chunk?: ResponseLoadSnapshotChunk;
    apply_snapshot_chunk?: ResponseApplySnapshotChunk;
    prepare_proposal?: ResponsePrepareProposal;
    process_proposal?: ResponseProcessProposal;
    extend_vote?: ResponseExtendVote;
    verify_vote_extension?: ResponseVerifyVoteExtension;
    finalize_block?: ResponseFinalizeBlock;
}
export interface ResponseException {
    error: string;
}
export interface ResponseEcho {
    message: string;
}
export type ResponseFlush = {};
export interface ResponseInfo {
    data: string;
    /** this is the software version of the application. TODO: remove? */
    version: string;
    app_version: number;
    last_block_height: number;
    last_block_app_hash: Uint8Array;
}
export interface ResponseInitChain {
    consensus_params?: ConsensusParams;
    validators: ValidatorUpdate[];
    app_hash: Uint8Array;
}
export interface ResponseQuery {
    code: number;
    /** bytes data = 2; // use "value" instead. */
    log: string;
    /** nondeterministic */
    info: string;
    index: number;
    key: Uint8Array;
    value: Uint8Array;
    proof_ops?: ProofOps;
    height: number;
    codespace: string;
}
export interface ResponseCheckTx {
    code: number;
    data: Uint8Array;
    gas_wanted: number;
    codespace: string;
    sender: string;
    priority: number;
}
export interface ResponseDeliverTx {
    code: number;
    data: Uint8Array;
    /** nondeterministic */
    log: string;
    /** nondeterministic */
    info: string;
    gas_wanted: number;
    gas_used: number;
    /** nondeterministic */
    events: Event[];
    codespace: string;
}
export interface ResponseCommit {
    /** reserve 1 */
    retain_height: number;
}
export interface ResponseListSnapshots {
    snapshots: Snapshot[];
}
export interface ResponseOfferSnapshot {
    result: ResponseOfferSnapshotResult;
}
export declare enum ResponseOfferSnapshotResult {
    /** UNKNOWN - Unknown result, abort all snapshot restoration */
    UNKNOWN = 0,
    /** ACCEPT - Snapshot accepted, apply chunks */
    ACCEPT = 1,
    /** ABORT - Abort all snapshot restoration */
    ABORT = 2,
    /** REJECT - Reject this specific snapshot, try others */
    REJECT = 3,
    /** REJECT_FORMAT - Reject all snapshots of this format, try others */
    REJECT_FORMAT = 4,
    /** REJECT_SENDER - Reject all snapshots from the sender(s), try others */
    REJECT_SENDER = 5,
    UNRECOGNIZED = -1
}
export interface ResponseLoadSnapshotChunk {
    chunk: Uint8Array;
}
export interface ResponseApplySnapshotChunk {
    result: ResponseApplySnapshotChunkResult;
    /** Chunks to refetch and reapply */
    refetch_chunks: number[];
    /** Chunk senders to reject and ban */
    reject_senders: string[];
}
export declare enum ResponseApplySnapshotChunkResult {
    /** UNKNOWN - Unknown result, abort all snapshot restoration */
    UNKNOWN = 0,
    /** ACCEPT - Chunk successfully accepted */
    ACCEPT = 1,
    /** ABORT - Abort all snapshot restoration */
    ABORT = 2,
    /** RETRY - Retry chunk (combine with refetch and reject) */
    RETRY = 3,
    /** RETRY_SNAPSHOT - Retry snapshot (combine with refetch and reject) */
    RETRY_SNAPSHOT = 4,
    /** REJECT_SNAPSHOT - Reject this snapshot, try others */
    REJECT_SNAPSHOT = 5,
    UNRECOGNIZED = -1
}
export interface ResponsePrepareProposal {
    tx_records: TxRecord[];
    app_hash: Uint8Array;
    tx_results: ExecTxResult[];
    validator_updates: ValidatorUpdate[];
    consensus_param_updates?: ConsensusParams;
}
export interface ResponseProcessProposal {
    status: ResponseProcessProposalProposalStatus;
    app_hash: Uint8Array;
    tx_results: ExecTxResult[];
    validator_updates: ValidatorUpdate[];
    consensus_param_updates?: ConsensusParams;
}
export declare enum ResponseProcessProposalProposalStatus {
    UNKNOWN = 0,
    ACCEPT = 1,
    REJECT = 2,
    UNRECOGNIZED = -1
}
export interface ResponseExtendVote {
    vote_extension: Uint8Array;
}
export interface ResponseVerifyVoteExtension {
    status: ResponseVerifyVoteExtensionVerifyStatus;
}
export declare enum ResponseVerifyVoteExtensionVerifyStatus {
    UNKNOWN = 0,
    ACCEPT = 1,
    REJECT = 2,
    UNRECOGNIZED = -1
}
export interface ResponseFinalizeBlock {
    events: Event[];
    tx_results: ExecTxResult[];
    validator_updates: ValidatorUpdate[];
    consensus_param_updates?: ConsensusParams;
    app_hash: Uint8Array;
}
export interface CommitInfo {
    round: number;
    votes: VoteInfo[];
}
export interface ExtendedCommitInfo {
    /** The round at which the block proposer decided in the previous height. */
    round: number;
    /**
     * List of validators' addresses in the last validator set with their voting
     * information, including vote extensions.
     */
    votes: ExtendedVoteInfo[];
}
export interface Event {
    type: string;
    attributes: EventAttribute[];
}
export interface EventAttribute {
    key: string;
    value: string;
    /** nondeterministic */
    index: boolean;
}
export interface ExecTxResult {
    code: number;
    data: Uint8Array;
    /** nondeterministic */
    log: string;
    /** nondeterministic */
    info: string;
    gas_wanted: number;
    gas_used: number;
    /** nondeterministic */
    events: Event[];
    codespace: string;
}
export interface TxResult {
    height: number;
    index: number;
    tx: Uint8Array;
    result?: ExecTxResult;
}
export interface TxRecord {
    action: TxRecordTxAction;
    tx: Uint8Array;
}
export declare enum TxRecordTxAction {
    /** UNKNOWN - Unknown action */
    UNKNOWN = 0,
    /** UNMODIFIED - The Application did not modify this transaction. */
    UNMODIFIED = 1,
    /** ADDED - The Application added this transaction. */
    ADDED = 2,
    /** REMOVED - The Application wants this transaction removed from the proposal and the mempool. */
    REMOVED = 3,
    UNRECOGNIZED = -1
}
export interface Validator {
    /** The first 20 bytes of SHA256(public key) */
    address: Uint8Array;
    /** PubKey pub_key = 2 [(gogoproto.nullable)=false]; */
    power: number;
}
export interface ValidatorUpdate {
    pub_key?: PublicKey;
    power: number;
}
export interface VoteInfo {
    validator?: Validator;
    signed_last_block: boolean;
}
export interface ExtendedVoteInfo {
    /** The validator that sent the vote. */
    validator?: Validator;
    /** Indicates whether the validator signed the last block, allowing for rewards based on validator availability. */
    signed_last_block: boolean;
    /** Non-deterministic extension provided by the sending validator's application. */
    vote_extension: Uint8Array;
}
export interface Misbehavior {
    type: MisbehaviorType;
    /** The offending validator */
    validator?: Validator;
    /** The height when the offense occurred */
    height: number;
    /** The corresponding time where the offense occurred */
    time?: Date;
    /**
     * Total voting power of the validator set in case the ABCI application does
     * not store historical validators.
     * https://github.com/tendermint/tendermint/issues/4581
     */
    total_voting_power: number;
}
export interface Snapshot {
    /** The height at which the snapshot was taken */
    height: number;
    /** The application-specific snapshot format */
    format: number;
    /** Number of chunks in the snapshot */
    chunks: number;
    /** Arbitrary snapshot hash, equal only if identical */
    hash: Uint8Array;
    /** Arbitrary application metadata */
    metadata: Uint8Array;
}
