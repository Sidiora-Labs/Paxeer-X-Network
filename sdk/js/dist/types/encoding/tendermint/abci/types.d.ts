import type { GeneratedType } from "@cosmjs/proto-signing";
import type { CommitInfo as CommitInfo_type, EventAttribute as EventAttribute_type, Event as Event_type, ExecTxResult as ExecTxResult_type, ExtendedCommitInfo as ExtendedCommitInfo_type, ExtendedVoteInfo as ExtendedVoteInfo_type, Misbehavior as Misbehavior_type, RequestApplySnapshotChunk as RequestApplySnapshotChunk_type, RequestCheckTx as RequestCheckTx_type, RequestCommit as RequestCommit_type, RequestEcho as RequestEcho_type, RequestExtendVote as RequestExtendVote_type, RequestFinalizeBlock as RequestFinalizeBlock_type, RequestFlush as RequestFlush_type, RequestInfo as RequestInfo_type, RequestInitChain as RequestInitChain_type, RequestListSnapshots as RequestListSnapshots_type, RequestLoadSnapshotChunk as RequestLoadSnapshotChunk_type, RequestOfferSnapshot as RequestOfferSnapshot_type, RequestPrepareProposal as RequestPrepareProposal_type, RequestProcessProposal as RequestProcessProposal_type, RequestQuery as RequestQuery_type, RequestVerifyVoteExtension as RequestVerifyVoteExtension_type, Request as Request_type, ResponseApplySnapshotChunk as ResponseApplySnapshotChunk_type, ResponseCheckTx as ResponseCheckTx_type, ResponseCommit as ResponseCommit_type, ResponseDeliverTx as ResponseDeliverTx_type, ResponseEcho as ResponseEcho_type, ResponseException as ResponseException_type, ResponseExtendVote as ResponseExtendVote_type, ResponseFinalizeBlock as ResponseFinalizeBlock_type, ResponseFlush as ResponseFlush_type, ResponseInfo as ResponseInfo_type, ResponseInitChain as ResponseInitChain_type, ResponseListSnapshots as ResponseListSnapshots_type, ResponseLoadSnapshotChunk as ResponseLoadSnapshotChunk_type, ResponseOfferSnapshot as ResponseOfferSnapshot_type, ResponsePrepareProposal as ResponsePrepareProposal_type, ResponseProcessProposal as ResponseProcessProposal_type, ResponseQuery as ResponseQuery_type, ResponseVerifyVoteExtension as ResponseVerifyVoteExtension_type, Response as Response_type, Snapshot as Snapshot_type, TxRecord as TxRecord_type, TxResult as TxResult_type, ValidatorUpdate as ValidatorUpdate_type, Validator as Validator_type, VoteInfo as VoteInfo_type } from "../../../types/tendermint/abci";
import { CheckTxType, MisbehaviorType, ResponseApplySnapshotChunkResult, ResponseOfferSnapshotResult, ResponseProcessProposalProposalStatus, ResponseVerifyVoteExtensionVerifyStatus, TxRecordTxAction } from "../../../types/tendermint/abci";
import type { MessageFns } from "../../common";
export interface Request extends Request_type {
}
export interface RequestEcho extends RequestEcho_type {
}
export interface RequestFlush extends RequestFlush_type {
}
export interface RequestInfo extends RequestInfo_type {
}
export interface RequestInitChain extends RequestInitChain_type {
}
export interface RequestQuery extends RequestQuery_type {
}
export interface RequestCheckTx extends RequestCheckTx_type {
}
export interface RequestCommit extends RequestCommit_type {
}
export interface RequestListSnapshots extends RequestListSnapshots_type {
}
export interface RequestOfferSnapshot extends RequestOfferSnapshot_type {
}
export interface RequestLoadSnapshotChunk extends RequestLoadSnapshotChunk_type {
}
export interface RequestApplySnapshotChunk extends RequestApplySnapshotChunk_type {
}
export interface RequestPrepareProposal extends RequestPrepareProposal_type {
}
export interface RequestProcessProposal extends RequestProcessProposal_type {
}
export interface RequestExtendVote extends RequestExtendVote_type {
}
export interface RequestVerifyVoteExtension extends RequestVerifyVoteExtension_type {
}
export interface RequestFinalizeBlock extends RequestFinalizeBlock_type {
}
export interface Response extends Response_type {
}
export interface ResponseException extends ResponseException_type {
}
export interface ResponseEcho extends ResponseEcho_type {
}
export interface ResponseFlush extends ResponseFlush_type {
}
export interface ResponseInfo extends ResponseInfo_type {
}
export interface ResponseInitChain extends ResponseInitChain_type {
}
export interface ResponseQuery extends ResponseQuery_type {
}
export interface ResponseCheckTx extends ResponseCheckTx_type {
}
export interface ResponseDeliverTx extends ResponseDeliverTx_type {
}
export interface ResponseCommit extends ResponseCommit_type {
}
export interface ResponseListSnapshots extends ResponseListSnapshots_type {
}
export interface ResponseOfferSnapshot extends ResponseOfferSnapshot_type {
}
export interface ResponseLoadSnapshotChunk extends ResponseLoadSnapshotChunk_type {
}
export interface ResponseApplySnapshotChunk extends ResponseApplySnapshotChunk_type {
}
export interface ResponsePrepareProposal extends ResponsePrepareProposal_type {
}
export interface ResponseProcessProposal extends ResponseProcessProposal_type {
}
export interface ResponseExtendVote extends ResponseExtendVote_type {
}
export interface ResponseVerifyVoteExtension extends ResponseVerifyVoteExtension_type {
}
export interface ResponseFinalizeBlock extends ResponseFinalizeBlock_type {
}
export interface CommitInfo extends CommitInfo_type {
}
export interface ExtendedCommitInfo extends ExtendedCommitInfo_type {
}
export interface Event extends Event_type {
}
export interface EventAttribute extends EventAttribute_type {
}
export interface ExecTxResult extends ExecTxResult_type {
}
export interface TxResult extends TxResult_type {
}
export interface TxRecord extends TxRecord_type {
}
export interface Validator extends Validator_type {
}
export interface ValidatorUpdate extends ValidatorUpdate_type {
}
export interface VoteInfo extends VoteInfo_type {
}
export interface ExtendedVoteInfo extends ExtendedVoteInfo_type {
}
export interface Misbehavior extends Misbehavior_type {
}
export interface Snapshot extends Snapshot_type {
}
export declare const Request: MessageFns<Request, "tendermint.abci.Request">;
export declare const RequestEcho: MessageFns<RequestEcho, "tendermint.abci.RequestEcho">;
export declare const RequestFlush: MessageFns<RequestFlush, "tendermint.abci.RequestFlush">;
export declare const RequestInfo: MessageFns<RequestInfo, "tendermint.abci.RequestInfo">;
export declare const RequestInitChain: MessageFns<RequestInitChain, "tendermint.abci.RequestInitChain">;
export declare const RequestQuery: MessageFns<RequestQuery, "tendermint.abci.RequestQuery">;
export declare const RequestCheckTx: MessageFns<RequestCheckTx, "tendermint.abci.RequestCheckTx">;
export declare const RequestCommit: MessageFns<RequestCommit, "tendermint.abci.RequestCommit">;
export declare const RequestListSnapshots: MessageFns<RequestListSnapshots, "tendermint.abci.RequestListSnapshots">;
export declare const RequestOfferSnapshot: MessageFns<RequestOfferSnapshot, "tendermint.abci.RequestOfferSnapshot">;
export declare const RequestLoadSnapshotChunk: MessageFns<RequestLoadSnapshotChunk, "tendermint.abci.RequestLoadSnapshotChunk">;
export declare const RequestApplySnapshotChunk: MessageFns<RequestApplySnapshotChunk, "tendermint.abci.RequestApplySnapshotChunk">;
export declare const RequestPrepareProposal: MessageFns<RequestPrepareProposal, "tendermint.abci.RequestPrepareProposal">;
export declare const RequestProcessProposal: MessageFns<RequestProcessProposal, "tendermint.abci.RequestProcessProposal">;
export declare const RequestExtendVote: MessageFns<RequestExtendVote, "tendermint.abci.RequestExtendVote">;
export declare const RequestVerifyVoteExtension: MessageFns<RequestVerifyVoteExtension, "tendermint.abci.RequestVerifyVoteExtension">;
export declare const RequestFinalizeBlock: MessageFns<RequestFinalizeBlock, "tendermint.abci.RequestFinalizeBlock">;
export declare const Response: MessageFns<Response, "tendermint.abci.Response">;
export declare const ResponseException: MessageFns<ResponseException, "tendermint.abci.ResponseException">;
export declare const ResponseEcho: MessageFns<ResponseEcho, "tendermint.abci.ResponseEcho">;
export declare const ResponseFlush: MessageFns<ResponseFlush, "tendermint.abci.ResponseFlush">;
export declare const ResponseInfo: MessageFns<ResponseInfo, "tendermint.abci.ResponseInfo">;
export declare const ResponseInitChain: MessageFns<ResponseInitChain, "tendermint.abci.ResponseInitChain">;
export declare const ResponseQuery: MessageFns<ResponseQuery, "tendermint.abci.ResponseQuery">;
export declare const ResponseCheckTx: MessageFns<ResponseCheckTx, "tendermint.abci.ResponseCheckTx">;
export declare const ResponseDeliverTx: MessageFns<ResponseDeliverTx, "tendermint.abci.ResponseDeliverTx">;
export declare const ResponseCommit: MessageFns<ResponseCommit, "tendermint.abci.ResponseCommit">;
export declare const ResponseListSnapshots: MessageFns<ResponseListSnapshots, "tendermint.abci.ResponseListSnapshots">;
export declare const ResponseOfferSnapshot: MessageFns<ResponseOfferSnapshot, "tendermint.abci.ResponseOfferSnapshot">;
export declare const ResponseLoadSnapshotChunk: MessageFns<ResponseLoadSnapshotChunk, "tendermint.abci.ResponseLoadSnapshotChunk">;
export declare const ResponseApplySnapshotChunk: MessageFns<ResponseApplySnapshotChunk, "tendermint.abci.ResponseApplySnapshotChunk">;
export declare const ResponsePrepareProposal: MessageFns<ResponsePrepareProposal, "tendermint.abci.ResponsePrepareProposal">;
export declare const ResponseProcessProposal: MessageFns<ResponseProcessProposal, "tendermint.abci.ResponseProcessProposal">;
export declare const ResponseExtendVote: MessageFns<ResponseExtendVote, "tendermint.abci.ResponseExtendVote">;
export declare const ResponseVerifyVoteExtension: MessageFns<ResponseVerifyVoteExtension, "tendermint.abci.ResponseVerifyVoteExtension">;
export declare const ResponseFinalizeBlock: MessageFns<ResponseFinalizeBlock, "tendermint.abci.ResponseFinalizeBlock">;
export declare const CommitInfo: MessageFns<CommitInfo, "tendermint.abci.CommitInfo">;
export declare const ExtendedCommitInfo: MessageFns<ExtendedCommitInfo, "tendermint.abci.ExtendedCommitInfo">;
export declare const Event: MessageFns<Event, "tendermint.abci.Event">;
export declare const EventAttribute: MessageFns<EventAttribute, "tendermint.abci.EventAttribute">;
export declare const ExecTxResult: MessageFns<ExecTxResult, "tendermint.abci.ExecTxResult">;
export declare const TxResult: MessageFns<TxResult, "tendermint.abci.TxResult">;
export declare const TxRecord: MessageFns<TxRecord, "tendermint.abci.TxRecord">;
export declare const Validator: MessageFns<Validator, "tendermint.abci.Validator">;
export declare const ValidatorUpdate: MessageFns<ValidatorUpdate, "tendermint.abci.ValidatorUpdate">;
export declare const VoteInfo: MessageFns<VoteInfo, "tendermint.abci.VoteInfo">;
export declare const ExtendedVoteInfo: MessageFns<ExtendedVoteInfo, "tendermint.abci.ExtendedVoteInfo">;
export declare const Misbehavior: MessageFns<Misbehavior, "tendermint.abci.Misbehavior">;
export declare const Snapshot: MessageFns<Snapshot, "tendermint.abci.Snapshot">;
export declare function checkTxTypeFromJSON(object: any): CheckTxType;
export declare function checkTxTypeToJSON(object: CheckTxType): string;
export declare function misbehaviorTypeFromJSON(object: any): MisbehaviorType;
export declare function misbehaviorTypeToJSON(object: MisbehaviorType): string;
export declare function responseOfferSnapshotResultFromJSON(object: any): ResponseOfferSnapshotResult;
export declare function responseOfferSnapshotResultToJSON(object: ResponseOfferSnapshotResult): string;
export declare function responseApplySnapshotChunkResultFromJSON(object: any): ResponseApplySnapshotChunkResult;
export declare function responseApplySnapshotChunkResultToJSON(object: ResponseApplySnapshotChunkResult): string;
export declare function responseProcessProposalProposalStatusFromJSON(object: any): ResponseProcessProposalProposalStatus;
export declare function responseProcessProposalProposalStatusToJSON(object: ResponseProcessProposalProposalStatus): string;
export declare function responseVerifyVoteExtensionVerifyStatusFromJSON(object: any): ResponseVerifyVoteExtensionVerifyStatus;
export declare function responseVerifyVoteExtensionVerifyStatusToJSON(object: ResponseVerifyVoteExtensionVerifyStatus): string;
export declare function txRecordTxActionFromJSON(object: any): TxRecordTxAction;
export declare function txRecordTxActionToJSON(object: TxRecordTxAction): string;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/tendermint.abci.Request": {
        aminoType: string;
        toAmino: (message: Request) => {
            echo?: RequestEcho_type;
            flush?: RequestFlush_type;
            info?: RequestInfo_type;
            init_chain?: RequestInitChain_type;
            query?: RequestQuery_type;
            check_tx?: RequestCheckTx_type;
            commit?: RequestCommit_type;
            list_snapshots?: RequestListSnapshots_type;
            offer_snapshot?: RequestOfferSnapshot_type;
            load_snapshot_chunk?: RequestLoadSnapshotChunk_type;
            apply_snapshot_chunk?: RequestApplySnapshotChunk_type;
            prepare_proposal?: RequestPrepareProposal_type;
            process_proposal?: RequestProcessProposal_type;
            extend_vote?: RequestExtendVote_type;
            verify_vote_extension?: RequestVerifyVoteExtension_type;
            finalize_block?: RequestFinalizeBlock_type;
        };
        fromAmino: (object: Request) => {
            echo?: RequestEcho_type;
            flush?: RequestFlush_type;
            info?: RequestInfo_type;
            init_chain?: RequestInitChain_type;
            query?: RequestQuery_type;
            check_tx?: RequestCheckTx_type;
            commit?: RequestCommit_type;
            list_snapshots?: RequestListSnapshots_type;
            offer_snapshot?: RequestOfferSnapshot_type;
            load_snapshot_chunk?: RequestLoadSnapshotChunk_type;
            apply_snapshot_chunk?: RequestApplySnapshotChunk_type;
            prepare_proposal?: RequestPrepareProposal_type;
            process_proposal?: RequestProcessProposal_type;
            extend_vote?: RequestExtendVote_type;
            verify_vote_extension?: RequestVerifyVoteExtension_type;
            finalize_block?: RequestFinalizeBlock_type;
        };
    };
    "/tendermint.abci.RequestEcho": {
        aminoType: string;
        toAmino: (message: RequestEcho) => {
            message: string;
        };
        fromAmino: (object: RequestEcho) => {
            message: string;
        };
    };
    "/tendermint.abci.RequestFlush": {
        aminoType: string;
        toAmino: (message: RequestFlush) => {};
        fromAmino: (object: RequestFlush) => {};
    };
    "/tendermint.abci.RequestInfo": {
        aminoType: string;
        toAmino: (message: RequestInfo) => {
            version: string;
            block_version: number;
            p2p_version: number;
            abci_version: string;
        };
        fromAmino: (object: RequestInfo) => {
            version: string;
            block_version: number;
            p2p_version: number;
            abci_version: string;
        };
    };
    "/tendermint.abci.RequestInitChain": {
        aminoType: string;
        toAmino: (message: RequestInitChain) => {
            time?: Date;
            chain_id: string;
            consensus_params?: import("../../../types/tendermint/types").ConsensusParams;
            validators: ValidatorUpdate_type[];
            app_state_bytes: Uint8Array;
            initial_height: number;
        };
        fromAmino: (object: RequestInitChain) => {
            time?: Date;
            chain_id: string;
            consensus_params?: import("../../../types/tendermint/types").ConsensusParams;
            validators: ValidatorUpdate_type[];
            app_state_bytes: Uint8Array;
            initial_height: number;
        };
    };
    "/tendermint.abci.RequestQuery": {
        aminoType: string;
        toAmino: (message: RequestQuery) => {
            data: Uint8Array;
            path: string;
            height: number;
            prove: boolean;
        };
        fromAmino: (object: RequestQuery) => {
            data: Uint8Array;
            path: string;
            height: number;
            prove: boolean;
        };
    };
    "/tendermint.abci.RequestCheckTx": {
        aminoType: string;
        toAmino: (message: RequestCheckTx) => {
            tx: Uint8Array;
            type: CheckTxType;
        };
        fromAmino: (object: RequestCheckTx) => {
            tx: Uint8Array;
            type: CheckTxType;
        };
    };
    "/tendermint.abci.RequestCommit": {
        aminoType: string;
        toAmino: (message: RequestCommit) => {};
        fromAmino: (object: RequestCommit) => {};
    };
    "/tendermint.abci.RequestListSnapshots": {
        aminoType: string;
        toAmino: (message: RequestListSnapshots) => {};
        fromAmino: (object: RequestListSnapshots) => {};
    };
    "/tendermint.abci.RequestOfferSnapshot": {
        aminoType: string;
        toAmino: (message: RequestOfferSnapshot) => {
            snapshot?: Snapshot_type;
            app_hash: Uint8Array;
        };
        fromAmino: (object: RequestOfferSnapshot) => {
            snapshot?: Snapshot_type;
            app_hash: Uint8Array;
        };
    };
    "/tendermint.abci.RequestLoadSnapshotChunk": {
        aminoType: string;
        toAmino: (message: RequestLoadSnapshotChunk) => {
            height: number;
            format: number;
            chunk: number;
        };
        fromAmino: (object: RequestLoadSnapshotChunk) => {
            height: number;
            format: number;
            chunk: number;
        };
    };
    "/tendermint.abci.RequestApplySnapshotChunk": {
        aminoType: string;
        toAmino: (message: RequestApplySnapshotChunk) => {
            index: number;
            chunk: Uint8Array;
            sender: string;
        };
        fromAmino: (object: RequestApplySnapshotChunk) => {
            index: number;
            chunk: Uint8Array;
            sender: string;
        };
    };
    "/tendermint.abci.RequestPrepareProposal": {
        aminoType: string;
        toAmino: (message: RequestPrepareProposal) => {
            max_tx_bytes: number;
            txs: Uint8Array[];
            local_last_commit?: ExtendedCommitInfo_type;
            byzantine_validators: Misbehavior_type[];
            height: number;
            time?: Date;
            next_validators_hash: Uint8Array;
            proposer_address: Uint8Array;
        };
        fromAmino: (object: RequestPrepareProposal) => {
            max_tx_bytes: number;
            txs: Uint8Array[];
            local_last_commit?: ExtendedCommitInfo_type;
            byzantine_validators: Misbehavior_type[];
            height: number;
            time?: Date;
            next_validators_hash: Uint8Array;
            proposer_address: Uint8Array;
        };
    };
    "/tendermint.abci.RequestProcessProposal": {
        aminoType: string;
        toAmino: (message: RequestProcessProposal) => {
            txs: Uint8Array[];
            proposed_last_commit?: CommitInfo_type;
            byzantine_validators: Misbehavior_type[];
            hash: Uint8Array;
            height: number;
            time?: Date;
            next_validators_hash: Uint8Array;
            proposer_address: Uint8Array;
        };
        fromAmino: (object: RequestProcessProposal) => {
            txs: Uint8Array[];
            proposed_last_commit?: CommitInfo_type;
            byzantine_validators: Misbehavior_type[];
            hash: Uint8Array;
            height: number;
            time?: Date;
            next_validators_hash: Uint8Array;
            proposer_address: Uint8Array;
        };
    };
    "/tendermint.abci.RequestExtendVote": {
        aminoType: string;
        toAmino: (message: RequestExtendVote) => {
            hash: Uint8Array;
            height: number;
        };
        fromAmino: (object: RequestExtendVote) => {
            hash: Uint8Array;
            height: number;
        };
    };
    "/tendermint.abci.RequestVerifyVoteExtension": {
        aminoType: string;
        toAmino: (message: RequestVerifyVoteExtension) => {
            hash: Uint8Array;
            validator_address: Uint8Array;
            height: number;
            vote_extension: Uint8Array;
        };
        fromAmino: (object: RequestVerifyVoteExtension) => {
            hash: Uint8Array;
            validator_address: Uint8Array;
            height: number;
            vote_extension: Uint8Array;
        };
    };
    "/tendermint.abci.RequestFinalizeBlock": {
        aminoType: string;
        toAmino: (message: RequestFinalizeBlock) => {
            txs: Uint8Array[];
            decided_last_commit?: CommitInfo_type;
            byzantine_validators: Misbehavior_type[];
            hash: Uint8Array;
            height: number;
            time?: Date;
            next_validators_hash: Uint8Array;
            proposer_address: Uint8Array;
        };
        fromAmino: (object: RequestFinalizeBlock) => {
            txs: Uint8Array[];
            decided_last_commit?: CommitInfo_type;
            byzantine_validators: Misbehavior_type[];
            hash: Uint8Array;
            height: number;
            time?: Date;
            next_validators_hash: Uint8Array;
            proposer_address: Uint8Array;
        };
    };
    "/tendermint.abci.Response": {
        aminoType: string;
        toAmino: (message: Response) => {
            exception?: ResponseException_type;
            echo?: ResponseEcho_type;
            flush?: ResponseFlush_type;
            info?: ResponseInfo_type;
            init_chain?: ResponseInitChain_type;
            query?: ResponseQuery_type;
            check_tx?: ResponseCheckTx_type;
            commit?: ResponseCommit_type;
            list_snapshots?: ResponseListSnapshots_type;
            offer_snapshot?: ResponseOfferSnapshot_type;
            load_snapshot_chunk?: ResponseLoadSnapshotChunk_type;
            apply_snapshot_chunk?: ResponseApplySnapshotChunk_type;
            prepare_proposal?: ResponsePrepareProposal_type;
            process_proposal?: ResponseProcessProposal_type;
            extend_vote?: ResponseExtendVote_type;
            verify_vote_extension?: ResponseVerifyVoteExtension_type;
            finalize_block?: ResponseFinalizeBlock_type;
        };
        fromAmino: (object: Response) => {
            exception?: ResponseException_type;
            echo?: ResponseEcho_type;
            flush?: ResponseFlush_type;
            info?: ResponseInfo_type;
            init_chain?: ResponseInitChain_type;
            query?: ResponseQuery_type;
            check_tx?: ResponseCheckTx_type;
            commit?: ResponseCommit_type;
            list_snapshots?: ResponseListSnapshots_type;
            offer_snapshot?: ResponseOfferSnapshot_type;
            load_snapshot_chunk?: ResponseLoadSnapshotChunk_type;
            apply_snapshot_chunk?: ResponseApplySnapshotChunk_type;
            prepare_proposal?: ResponsePrepareProposal_type;
            process_proposal?: ResponseProcessProposal_type;
            extend_vote?: ResponseExtendVote_type;
            verify_vote_extension?: ResponseVerifyVoteExtension_type;
            finalize_block?: ResponseFinalizeBlock_type;
        };
    };
    "/tendermint.abci.ResponseException": {
        aminoType: string;
        toAmino: (message: ResponseException) => {
            error: string;
        };
        fromAmino: (object: ResponseException) => {
            error: string;
        };
    };
    "/tendermint.abci.ResponseEcho": {
        aminoType: string;
        toAmino: (message: ResponseEcho) => {
            message: string;
        };
        fromAmino: (object: ResponseEcho) => {
            message: string;
        };
    };
    "/tendermint.abci.ResponseFlush": {
        aminoType: string;
        toAmino: (message: ResponseFlush) => {};
        fromAmino: (object: ResponseFlush) => {};
    };
    "/tendermint.abci.ResponseInfo": {
        aminoType: string;
        toAmino: (message: ResponseInfo) => {
            data: string;
            version: string;
            app_version: number;
            last_block_height: number;
            last_block_app_hash: Uint8Array;
        };
        fromAmino: (object: ResponseInfo) => {
            data: string;
            version: string;
            app_version: number;
            last_block_height: number;
            last_block_app_hash: Uint8Array;
        };
    };
    "/tendermint.abci.ResponseInitChain": {
        aminoType: string;
        toAmino: (message: ResponseInitChain) => {
            consensus_params?: import("../../../types/tendermint/types").ConsensusParams;
            validators: ValidatorUpdate_type[];
            app_hash: Uint8Array;
        };
        fromAmino: (object: ResponseInitChain) => {
            consensus_params?: import("../../../types/tendermint/types").ConsensusParams;
            validators: ValidatorUpdate_type[];
            app_hash: Uint8Array;
        };
    };
    "/tendermint.abci.ResponseQuery": {
        aminoType: string;
        toAmino: (message: ResponseQuery) => {
            code: number;
            log: string;
            info: string;
            index: number;
            key: Uint8Array;
            value: Uint8Array;
            proof_ops?: import("../../../types/tendermint/crypto").ProofOps;
            height: number;
            codespace: string;
        };
        fromAmino: (object: ResponseQuery) => {
            code: number;
            log: string;
            info: string;
            index: number;
            key: Uint8Array;
            value: Uint8Array;
            proof_ops?: import("../../../types/tendermint/crypto").ProofOps;
            height: number;
            codespace: string;
        };
    };
    "/tendermint.abci.ResponseCheckTx": {
        aminoType: string;
        toAmino: (message: ResponseCheckTx) => {
            code: number;
            data: Uint8Array;
            gas_wanted: number;
            codespace: string;
            sender: string;
            priority: number;
        };
        fromAmino: (object: ResponseCheckTx) => {
            code: number;
            data: Uint8Array;
            gas_wanted: number;
            codespace: string;
            sender: string;
            priority: number;
        };
    };
    "/tendermint.abci.ResponseDeliverTx": {
        aminoType: string;
        toAmino: (message: ResponseDeliverTx) => {
            code: number;
            data: Uint8Array;
            log: string;
            info: string;
            gas_wanted: number;
            gas_used: number;
            events: Event_type[];
            codespace: string;
        };
        fromAmino: (object: ResponseDeliverTx) => {
            code: number;
            data: Uint8Array;
            log: string;
            info: string;
            gas_wanted: number;
            gas_used: number;
            events: Event_type[];
            codespace: string;
        };
    };
    "/tendermint.abci.ResponseCommit": {
        aminoType: string;
        toAmino: (message: ResponseCommit) => {
            retain_height: number;
        };
        fromAmino: (object: ResponseCommit) => {
            retain_height: number;
        };
    };
    "/tendermint.abci.ResponseListSnapshots": {
        aminoType: string;
        toAmino: (message: ResponseListSnapshots) => {
            snapshots: Snapshot_type[];
        };
        fromAmino: (object: ResponseListSnapshots) => {
            snapshots: Snapshot_type[];
        };
    };
    "/tendermint.abci.ResponseOfferSnapshot": {
        aminoType: string;
        toAmino: (message: ResponseOfferSnapshot) => {
            result: ResponseOfferSnapshotResult;
        };
        fromAmino: (object: ResponseOfferSnapshot) => {
            result: ResponseOfferSnapshotResult;
        };
    };
    "/tendermint.abci.ResponseLoadSnapshotChunk": {
        aminoType: string;
        toAmino: (message: ResponseLoadSnapshotChunk) => {
            chunk: Uint8Array;
        };
        fromAmino: (object: ResponseLoadSnapshotChunk) => {
            chunk: Uint8Array;
        };
    };
    "/tendermint.abci.ResponseApplySnapshotChunk": {
        aminoType: string;
        toAmino: (message: ResponseApplySnapshotChunk) => {
            result: ResponseApplySnapshotChunkResult;
            refetch_chunks: number[];
            reject_senders: string[];
        };
        fromAmino: (object: ResponseApplySnapshotChunk) => {
            result: ResponseApplySnapshotChunkResult;
            refetch_chunks: number[];
            reject_senders: string[];
        };
    };
    "/tendermint.abci.ResponsePrepareProposal": {
        aminoType: string;
        toAmino: (message: ResponsePrepareProposal) => {
            tx_records: TxRecord_type[];
            app_hash: Uint8Array;
            tx_results: ExecTxResult_type[];
            validator_updates: ValidatorUpdate_type[];
            consensus_param_updates?: import("../../../types/tendermint/types").ConsensusParams;
        };
        fromAmino: (object: ResponsePrepareProposal) => {
            tx_records: TxRecord_type[];
            app_hash: Uint8Array;
            tx_results: ExecTxResult_type[];
            validator_updates: ValidatorUpdate_type[];
            consensus_param_updates?: import("../../../types/tendermint/types").ConsensusParams;
        };
    };
    "/tendermint.abci.ResponseProcessProposal": {
        aminoType: string;
        toAmino: (message: ResponseProcessProposal) => {
            status: ResponseProcessProposalProposalStatus;
            app_hash: Uint8Array;
            tx_results: ExecTxResult_type[];
            validator_updates: ValidatorUpdate_type[];
            consensus_param_updates?: import("../../../types/tendermint/types").ConsensusParams;
        };
        fromAmino: (object: ResponseProcessProposal) => {
            status: ResponseProcessProposalProposalStatus;
            app_hash: Uint8Array;
            tx_results: ExecTxResult_type[];
            validator_updates: ValidatorUpdate_type[];
            consensus_param_updates?: import("../../../types/tendermint/types").ConsensusParams;
        };
    };
    "/tendermint.abci.ResponseExtendVote": {
        aminoType: string;
        toAmino: (message: ResponseExtendVote) => {
            vote_extension: Uint8Array;
        };
        fromAmino: (object: ResponseExtendVote) => {
            vote_extension: Uint8Array;
        };
    };
    "/tendermint.abci.ResponseVerifyVoteExtension": {
        aminoType: string;
        toAmino: (message: ResponseVerifyVoteExtension) => {
            status: ResponseVerifyVoteExtensionVerifyStatus;
        };
        fromAmino: (object: ResponseVerifyVoteExtension) => {
            status: ResponseVerifyVoteExtensionVerifyStatus;
        };
    };
    "/tendermint.abci.ResponseFinalizeBlock": {
        aminoType: string;
        toAmino: (message: ResponseFinalizeBlock) => {
            events: Event_type[];
            tx_results: ExecTxResult_type[];
            validator_updates: ValidatorUpdate_type[];
            consensus_param_updates?: import("../../../types/tendermint/types").ConsensusParams;
            app_hash: Uint8Array;
        };
        fromAmino: (object: ResponseFinalizeBlock) => {
            events: Event_type[];
            tx_results: ExecTxResult_type[];
            validator_updates: ValidatorUpdate_type[];
            consensus_param_updates?: import("../../../types/tendermint/types").ConsensusParams;
            app_hash: Uint8Array;
        };
    };
    "/tendermint.abci.CommitInfo": {
        aminoType: string;
        toAmino: (message: CommitInfo) => {
            round: number;
            votes: VoteInfo_type[];
        };
        fromAmino: (object: CommitInfo) => {
            round: number;
            votes: VoteInfo_type[];
        };
    };
    "/tendermint.abci.ExtendedCommitInfo": {
        aminoType: string;
        toAmino: (message: ExtendedCommitInfo) => {
            round: number;
            votes: ExtendedVoteInfo_type[];
        };
        fromAmino: (object: ExtendedCommitInfo) => {
            round: number;
            votes: ExtendedVoteInfo_type[];
        };
    };
    "/tendermint.abci.Event": {
        aminoType: string;
        toAmino: (message: Event) => {
            type: string;
            attributes: EventAttribute_type[];
        };
        fromAmino: (object: Event) => {
            type: string;
            attributes: EventAttribute_type[];
        };
    };
    "/tendermint.abci.EventAttribute": {
        aminoType: string;
        toAmino: (message: EventAttribute) => {
            key: string;
            value: string;
            index: boolean;
        };
        fromAmino: (object: EventAttribute) => {
            key: string;
            value: string;
            index: boolean;
        };
    };
    "/tendermint.abci.ExecTxResult": {
        aminoType: string;
        toAmino: (message: ExecTxResult) => {
            code: number;
            data: Uint8Array;
            log: string;
            info: string;
            gas_wanted: number;
            gas_used: number;
            events: Event_type[];
            codespace: string;
        };
        fromAmino: (object: ExecTxResult) => {
            code: number;
            data: Uint8Array;
            log: string;
            info: string;
            gas_wanted: number;
            gas_used: number;
            events: Event_type[];
            codespace: string;
        };
    };
    "/tendermint.abci.TxResult": {
        aminoType: string;
        toAmino: (message: TxResult) => {
            height: number;
            index: number;
            tx: Uint8Array;
            result?: ExecTxResult_type;
        };
        fromAmino: (object: TxResult) => {
            height: number;
            index: number;
            tx: Uint8Array;
            result?: ExecTxResult_type;
        };
    };
    "/tendermint.abci.TxRecord": {
        aminoType: string;
        toAmino: (message: TxRecord) => {
            action: TxRecordTxAction;
            tx: Uint8Array;
        };
        fromAmino: (object: TxRecord) => {
            action: TxRecordTxAction;
            tx: Uint8Array;
        };
    };
    "/tendermint.abci.Validator": {
        aminoType: string;
        toAmino: (message: Validator) => {
            address: Uint8Array;
            power: number;
        };
        fromAmino: (object: Validator) => {
            address: Uint8Array;
            power: number;
        };
    };
    "/tendermint.abci.ValidatorUpdate": {
        aminoType: string;
        toAmino: (message: ValidatorUpdate) => {
            pub_key?: import("../../../types/tendermint/crypto").PublicKey;
            power: number;
        };
        fromAmino: (object: ValidatorUpdate) => {
            pub_key?: import("../../../types/tendermint/crypto").PublicKey;
            power: number;
        };
    };
    "/tendermint.abci.VoteInfo": {
        aminoType: string;
        toAmino: (message: VoteInfo) => {
            validator?: Validator_type;
            signed_last_block: boolean;
        };
        fromAmino: (object: VoteInfo) => {
            validator?: Validator_type;
            signed_last_block: boolean;
        };
    };
    "/tendermint.abci.ExtendedVoteInfo": {
        aminoType: string;
        toAmino: (message: ExtendedVoteInfo) => {
            validator?: Validator_type;
            signed_last_block: boolean;
            vote_extension: Uint8Array;
        };
        fromAmino: (object: ExtendedVoteInfo) => {
            validator?: Validator_type;
            signed_last_block: boolean;
            vote_extension: Uint8Array;
        };
    };
    "/tendermint.abci.Misbehavior": {
        aminoType: string;
        toAmino: (message: Misbehavior) => {
            type: MisbehaviorType;
            validator?: Validator_type;
            height: number;
            time?: Date;
            total_voting_power: number;
        };
        fromAmino: (object: Misbehavior) => {
            type: MisbehaviorType;
            validator?: Validator_type;
            height: number;
            time?: Date;
            total_voting_power: number;
        };
    };
    "/tendermint.abci.Snapshot": {
        aminoType: string;
        toAmino: (message: Snapshot) => {
            height: number;
            format: number;
            chunks: number;
            hash: Uint8Array;
            metadata: Uint8Array;
        };
        fromAmino: (object: Snapshot) => {
            height: number;
            format: number;
            chunks: number;
            hash: Uint8Array;
            metadata: Uint8Array;
        };
    };
};
