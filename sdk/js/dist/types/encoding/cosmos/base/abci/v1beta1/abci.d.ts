import type { GeneratedType } from "@cosmjs/proto-signing";
import type { ABCIMessageLog as ABCIMessageLog_type, Attribute as Attribute_type, GasInfo as GasInfo_type, MsgData as MsgData_type, Result as Result_type, SearchTxsResult as SearchTxsResult_type, SimulationResponse as SimulationResponse_type, StringEvent as StringEvent_type, TxMsgData as TxMsgData_type, TxResponse as TxResponse_type } from "../../../../../types/cosmos/base/abci/v1beta1";
import type { MessageFns } from "../../../../common";
export interface TxResponse extends TxResponse_type {
}
export interface ABCIMessageLog extends ABCIMessageLog_type {
}
export interface StringEvent extends StringEvent_type {
}
export interface Attribute extends Attribute_type {
}
export interface GasInfo extends GasInfo_type {
}
export interface Result extends Result_type {
}
export interface SimulationResponse extends SimulationResponse_type {
}
export interface MsgData extends MsgData_type {
}
export interface TxMsgData extends TxMsgData_type {
}
export interface SearchTxsResult extends SearchTxsResult_type {
}
export declare const TxResponse: MessageFns<TxResponse, "cosmos.base.abci.v1beta1.TxResponse">;
export declare const ABCIMessageLog: MessageFns<ABCIMessageLog, "cosmos.base.abci.v1beta1.ABCIMessageLog">;
export declare const StringEvent: MessageFns<StringEvent, "cosmos.base.abci.v1beta1.StringEvent">;
export declare const Attribute: MessageFns<Attribute, "cosmos.base.abci.v1beta1.Attribute">;
export declare const GasInfo: MessageFns<GasInfo, "cosmos.base.abci.v1beta1.GasInfo">;
export declare const Result: MessageFns<Result, "cosmos.base.abci.v1beta1.Result">;
export declare const SimulationResponse: MessageFns<SimulationResponse, "cosmos.base.abci.v1beta1.SimulationResponse">;
export declare const MsgData: MessageFns<MsgData, "cosmos.base.abci.v1beta1.MsgData">;
export declare const TxMsgData: MessageFns<TxMsgData, "cosmos.base.abci.v1beta1.TxMsgData">;
export declare const SearchTxsResult: MessageFns<SearchTxsResult, "cosmos.base.abci.v1beta1.SearchTxsResult">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.base.abci.v1beta1.TxResponse": {
        aminoType: string;
        toAmino: (message: TxResponse) => {
            height: number;
            txhash: string;
            codespace: string;
            code: number;
            data: string;
            raw_log: string;
            logs: ABCIMessageLog_type[];
            info: string;
            gas_wanted: number;
            gas_used: number;
            tx?: import("../../../../../types/google/protobuf").Any;
            timestamp: string;
            events: import("../../../../../types/tendermint/abci").Event[];
        };
        fromAmino: (object: TxResponse) => {
            height: number;
            txhash: string;
            codespace: string;
            code: number;
            data: string;
            raw_log: string;
            logs: ABCIMessageLog_type[];
            info: string;
            gas_wanted: number;
            gas_used: number;
            tx?: import("../../../../../types/google/protobuf").Any;
            timestamp: string;
            events: import("../../../../../types/tendermint/abci").Event[];
        };
    };
    "/cosmos.base.abci.v1beta1.ABCIMessageLog": {
        aminoType: string;
        toAmino: (message: ABCIMessageLog) => {
            msg_index: number;
            log: string;
            events: StringEvent_type[];
        };
        fromAmino: (object: ABCIMessageLog) => {
            msg_index: number;
            log: string;
            events: StringEvent_type[];
        };
    };
    "/cosmos.base.abci.v1beta1.StringEvent": {
        aminoType: string;
        toAmino: (message: StringEvent) => {
            type: string;
            attributes: Attribute_type[];
        };
        fromAmino: (object: StringEvent) => {
            type: string;
            attributes: Attribute_type[];
        };
    };
    "/cosmos.base.abci.v1beta1.Attribute": {
        aminoType: string;
        toAmino: (message: Attribute) => {
            key: string;
            value: string;
        };
        fromAmino: (object: Attribute) => {
            key: string;
            value: string;
        };
    };
    "/cosmos.base.abci.v1beta1.GasInfo": {
        aminoType: string;
        toAmino: (message: GasInfo) => {
            gas_wanted: number;
            gas_used: number;
            gas_estimate: number;
        };
        fromAmino: (object: GasInfo) => {
            gas_wanted: number;
            gas_used: number;
            gas_estimate: number;
        };
    };
    "/cosmos.base.abci.v1beta1.Result": {
        aminoType: string;
        toAmino: (message: Result) => {
            data: Uint8Array;
            log: string;
            events: import("../../../../../types/tendermint/abci").Event[];
            evmError: string;
        };
        fromAmino: (object: Result) => {
            data: Uint8Array;
            log: string;
            events: import("../../../../../types/tendermint/abci").Event[];
            evmError: string;
        };
    };
    "/cosmos.base.abci.v1beta1.SimulationResponse": {
        aminoType: string;
        toAmino: (message: SimulationResponse) => {
            gas_info?: GasInfo_type;
            result?: Result_type;
        };
        fromAmino: (object: SimulationResponse) => {
            gas_info?: GasInfo_type;
            result?: Result_type;
        };
    };
    "/cosmos.base.abci.v1beta1.MsgData": {
        aminoType: string;
        toAmino: (message: MsgData) => {
            msg_type: string;
            data: Uint8Array;
        };
        fromAmino: (object: MsgData) => {
            msg_type: string;
            data: Uint8Array;
        };
    };
    "/cosmos.base.abci.v1beta1.TxMsgData": {
        aminoType: string;
        toAmino: (message: TxMsgData) => {
            data: MsgData_type[];
        };
        fromAmino: (object: TxMsgData) => {
            data: MsgData_type[];
        };
    };
    "/cosmos.base.abci.v1beta1.SearchTxsResult": {
        aminoType: string;
        toAmino: (message: SearchTxsResult) => {
            total_count: number;
            count: number;
            page_number: number;
            page_total: number;
            limit: number;
            txs: TxResponse_type[];
        };
        fromAmino: (object: SearchTxsResult) => {
            total_count: number;
            count: number;
            page_number: number;
            page_total: number;
            limit: number;
            txs: TxResponse_type[];
        };
    };
};
