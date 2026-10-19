import type { Any } from "../../../../google/protobuf/any";
import type { Event } from "../../../../tendermint/abci/types";
export interface TxResponse {
    /** The block height */
    height: number;
    /** The transaction hash. */
    txhash: string;
    /** Namespace for the Code */
    codespace: string;
    /** Response code. */
    code: number;
    /** Result bytes, if any. */
    data: string;
    /**
     * The output of the application's logger (raw string). May be
     * non-deterministic.
     */
    raw_log: string;
    /** The output of the application's logger (typed). May be non-deterministic. */
    logs: ABCIMessageLog[];
    /** Additional information. May be non-deterministic. */
    info: string;
    /** Amount of gas requested for transaction. */
    gas_wanted: number;
    /** Amount of gas consumed by transaction. */
    gas_used: number;
    /** The request transaction bytes. */
    tx?: Any;
    /**
     * Time of the previous block. For heights > 1, it's the weighted median of
     * the timestamps of the valid votes in the block.LastCommit. For height == 1,
     * it's genesis time.
     */
    timestamp: string;
    /**
     * Events defines all the events emitted by processing a transaction. Note,
     * these events include those emitted by processing all the messages and those
     * emitted from the ante handler. Whereas Logs contains the events, with
     * additional metadata, emitted only by processing the messages.
     *
     * Since: cosmos-sdk 0.42.11, 0.44.5, 0.45
     */
    events: Event[];
}
export interface ABCIMessageLog {
    msg_index: number;
    log: string;
    /**
     * Events contains a slice of Event objects that were emitted during some
     * execution.
     */
    events: StringEvent[];
}
export interface StringEvent {
    type: string;
    attributes: Attribute[];
}
export interface Attribute {
    key: string;
    value: string;
}
export interface GasInfo {
    /** GasWanted is the maximum units of work we allow this tx to perform. */
    gas_wanted: number;
    /** GasUsed is the amount of gas actually consumed. */
    gas_used: number;
    /** GasEstimate is the estimated gas used by the transaction. */
    gas_estimate: number;
}
export interface Result {
    /**
     * Data is any data returned from message or handler execution. It MUST be
     * length prefixed in order to separate data from multiple message executions.
     */
    data: Uint8Array;
    /** Log contains the log information from message or handler execution. */
    log: string;
    /**
     * Events contains a slice of Event objects that were emitted during message
     * or handler execution.
     */
    events: Event[];
    /** EVM VM error during execution */
    evmError: string;
}
export interface SimulationResponse {
    gas_info?: GasInfo;
    result?: Result;
}
export interface MsgData {
    msg_type: string;
    data: Uint8Array;
}
export interface TxMsgData {
    data: MsgData[];
}
export interface SearchTxsResult {
    /** Count of all txs */
    total_count: number;
    /** Count of txs in current page */
    count: number;
    /** Index of current page, start from 1 */
    page_number: number;
    /** Count of total pages */
    page_total: number;
    /** Max count txs per page */
    limit: number;
    /** List of txs in current page */
    txs: TxResponse[];
}
