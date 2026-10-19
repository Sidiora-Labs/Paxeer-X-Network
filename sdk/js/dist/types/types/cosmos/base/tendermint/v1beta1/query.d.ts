import type { Any } from "../../../../google/protobuf/any";
import type { NodeInfo } from "../../../../tendermint/p2p/types";
import type { Block } from "../../../../tendermint/types/block";
import type { BlockID } from "../../../../tendermint/types/types";
import type { PageRequest, PageResponse } from "../../query/v1beta1/pagination";
export interface GetValidatorSetByHeightRequest {
    height: number;
    /** pagination defines an pagination for the request. */
    pagination?: PageRequest;
}
export interface GetValidatorSetByHeightResponse {
    block_height: number;
    validators: Validator[];
    /** pagination defines an pagination for the response. */
    pagination?: PageResponse;
}
export interface GetLatestValidatorSetRequest {
    /** pagination defines an pagination for the request. */
    pagination?: PageRequest;
}
export interface GetLatestValidatorSetResponse {
    block_height: number;
    validators: Validator[];
    /** pagination defines an pagination for the response. */
    pagination?: PageResponse;
}
export interface Validator {
    address: string;
    pub_key?: Any;
    voting_power: number;
    proposer_priority: number;
}
export interface GetBlockByHeightRequest {
    height: number;
}
export interface GetBlockByHeightResponse {
    block_id?: BlockID;
    block?: Block;
}
export type GetLatestBlockRequest = {};
export interface GetLatestBlockResponse {
    block_id?: BlockID;
    block?: Block;
}
export type GetSyncingRequest = {};
export interface GetSyncingResponse {
    syncing: boolean;
}
export type GetNodeInfoRequest = {};
export interface GetNodeInfoResponse {
    default_node_info?: NodeInfo;
    application_version?: VersionInfo;
}
export interface VersionInfo {
    name: string;
    app_name: string;
    version: string;
    git_commit: string;
    build_tags: string;
    go_version: string;
    build_deps: Module[];
    /** Since: cosmos-sdk 0.43 */
    cosmos_sdk_version: string;
}
export interface Module {
    /** module path */
    path: string;
    /** module version */
    version: string;
    /** checksum */
    sum: string;
}
