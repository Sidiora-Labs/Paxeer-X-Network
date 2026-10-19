import type { Block } from "../../../tendermint/types/block";
import type { BlockID } from "../../../tendermint/types/types";
import type { GasInfo, Result, TxResponse } from "../../base/abci/v1beta1/abci";
import type { PageRequest, PageResponse } from "../../base/query/v1beta1/pagination";
import type { Tx } from "./tx";
export declare enum OrderBy {
    /** ORDER_BY_UNSPECIFIED - ORDER_BY_UNSPECIFIED specifies an unknown sorting order. OrderBy defaults to ASC in this case. */
    ORDER_BY_UNSPECIFIED = 0,
    /** ORDER_BY_ASC - ORDER_BY_ASC defines ascending order */
    ORDER_BY_ASC = 1,
    /** ORDER_BY_DESC - ORDER_BY_DESC defines descending order */
    ORDER_BY_DESC = 2,
    UNRECOGNIZED = -1
}
export declare enum BroadcastMode {
    /** BROADCAST_MODE_UNSPECIFIED - zero-value for mode ordering */
    BROADCAST_MODE_UNSPECIFIED = 0,
    /**
     * BROADCAST_MODE_BLOCK - BROADCAST_MODE_BLOCK defines a tx broadcasting mode where the client waits for
     * the tx to be committed in a block.
     */
    BROADCAST_MODE_BLOCK = 1,
    /**
     * BROADCAST_MODE_SYNC - BROADCAST_MODE_SYNC defines a tx broadcasting mode where the client waits for
     * a CheckTx execution response only.
     */
    BROADCAST_MODE_SYNC = 2,
    /**
     * BROADCAST_MODE_ASYNC - BROADCAST_MODE_ASYNC defines a tx broadcasting mode where the client returns
     * immediately.
     */
    BROADCAST_MODE_ASYNC = 3,
    UNRECOGNIZED = -1
}
export interface GetTxsEventRequest {
    /** events is the list of transaction event type. */
    events: string[];
    /** pagination defines a pagination for the request. */
    pagination?: PageRequest;
    order_by: OrderBy;
}
export interface GetTxsEventResponse {
    /** txs is the list of queried transactions. */
    txs: Tx[];
    /** tx_responses is the list of queried TxResponses. */
    tx_responses: TxResponse[];
    /** pagination defines a pagination for the response. */
    pagination?: PageResponse;
}
export interface BroadcastTxRequest {
    /** tx_bytes is the raw transaction. */
    tx_bytes: Uint8Array;
    mode: BroadcastMode;
}
export interface BroadcastTxResponse {
    /** tx_response is the queried TxResponses. */
    tx_response?: TxResponse;
}
export interface SimulateRequest {
    /**
     * tx is the transaction to simulate.
     * Deprecated. Send raw tx bytes instead.
     *
     * @deprecated
     */
    tx?: Tx;
    /**
     * tx_bytes is the raw transaction.
     *
     * Since: cosmos-sdk 0.43
     */
    tx_bytes: Uint8Array;
}
export interface SimulateResponse {
    /** gas_info is the information about gas used in the simulation. */
    gas_info?: GasInfo;
    /** result is the result of the simulation. */
    result?: Result;
}
export interface GetTxRequest {
    /** hash is the tx hash to query, encoded as a hex string. */
    hash: string;
}
export interface GetTxResponse {
    /** tx is the queried transaction. */
    tx?: Tx;
    /** tx_response is the queried TxResponses. */
    tx_response?: TxResponse;
}
export interface GetBlockWithTxsRequest {
    /** height is the height of the block to query. */
    height: number;
    /** pagination defines a pagination for the request. */
    pagination?: PageRequest;
}
export interface GetBlockWithTxsResponse {
    /** txs are the transactions in the block. */
    txs: Tx[];
    block_id?: BlockID;
    block?: Block;
    /** pagination defines a pagination for the response. */
    pagination?: PageResponse;
}
