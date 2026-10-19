import type { Proof } from "../crypto/proof";
import type { Consensus } from "../version/types";
import type { ValidatorSet } from "./validator";
export declare enum BlockIDFlag {
    BLOCK_ID_FLAG_UNKNOWN = 0,
    BLOCK_ID_FLAG_ABSENT = 1,
    BLOCK_ID_FLAG_COMMIT = 2,
    BLOCK_ID_FLAG_NIL = 3,
    UNRECOGNIZED = -1
}
export declare enum SignedMsgType {
    SIGNED_MSG_TYPE_UNKNOWN = 0,
    /** SIGNED_MSG_TYPE_PREVOTE - Votes */
    SIGNED_MSG_TYPE_PREVOTE = 1,
    SIGNED_MSG_TYPE_PRECOMMIT = 2,
    /** SIGNED_MSG_TYPE_PROPOSAL - Proposals */
    SIGNED_MSG_TYPE_PROPOSAL = 32,
    UNRECOGNIZED = -1
}
export interface PartSetHeader {
    total: number;
    hash: Uint8Array;
}
export interface Part {
    index: number;
    bytes: Uint8Array;
    proof?: Proof;
}
export interface BlockID {
    hash: Uint8Array;
    part_set_header?: PartSetHeader;
}
export interface Header {
    /** basic block info */
    version?: Consensus;
    chain_id: string;
    height: number;
    time?: Date;
    /** prev block info */
    last_block_id?: BlockID;
    /** hashes of block data */
    last_commit_hash: Uint8Array;
    /** transactions */
    data_hash: Uint8Array;
    /** hashes from the app output from the prev block */
    validators_hash: Uint8Array;
    /** validators for the next block */
    next_validators_hash: Uint8Array;
    /** consensus params for current block */
    consensus_hash: Uint8Array;
    /** state after txs from the previous block */
    app_hash: Uint8Array;
    /** root hash of all results from the txs from the previous block */
    last_results_hash: Uint8Array;
    /** consensus info */
    evidence_hash: Uint8Array;
    /** original proposer of the block */
    proposer_address: Uint8Array;
}
export interface Data {
    /**
     * Txs that will be applied by state @ block.Height+1.
     * NOTE: not all txs here are valid.  We're just agreeing on the order first.
     * This means that block.AppHash does not include these txs.
     */
    txs: Uint8Array[];
}
export interface Vote {
    type: SignedMsgType;
    height: number;
    round: number;
    /** zero if vote is nil. */
    block_id?: BlockID;
    timestamp?: Date;
    validator_address: Uint8Array;
    validator_index: number;
    /**
     * Vote signature by the validator if they participated in consensus for the
     * associated block.
     */
    signature: Uint8Array;
    /**
     * Vote extension provided by the application. Only valid for precommit
     * messages.
     */
    extension: Uint8Array;
    /**
     * Vote extension signature by the validator if they participated in
     * consensus for the associated block. Only valid for precommit messages.
     */
    extension_signature: Uint8Array;
}
export interface Commit {
    height: number;
    round: number;
    block_id?: BlockID;
    signatures: CommitSig[];
}
export interface CommitSig {
    block_id_flag: BlockIDFlag;
    validator_address: Uint8Array;
    timestamp?: Date;
    signature: Uint8Array;
}
export interface ExtendedCommit {
    height: number;
    round: number;
    block_id?: BlockID;
    extended_signatures: ExtendedCommitSig[];
}
export interface ExtendedCommitSig {
    block_id_flag: BlockIDFlag;
    validator_address: Uint8Array;
    timestamp?: Date;
    signature: Uint8Array;
    /** Vote extension data */
    extension: Uint8Array;
    /** Vote extension signature */
    extension_signature: Uint8Array;
}
export interface Proposal {
    type: SignedMsgType;
    height: number;
    round: number;
    pol_round: number;
    block_id?: BlockID;
    timestamp?: Date;
    signature: Uint8Array;
}
export interface SignedHeader {
    header?: Header;
    commit?: Commit;
}
export interface LightBlock {
    signed_header?: SignedHeader;
    validator_set?: ValidatorSet;
}
export interface BlockMeta {
    block_id?: BlockID;
    block_size: number;
    header?: Header;
    num_txs: number;
}
export interface TxProof {
    root_hash: Uint8Array;
    data: Uint8Array;
    proof?: Proof;
}
