import { BinaryReader, BinaryWriter } from "@bufbuild/protobuf/wire";
import { Block } from "../../../tendermint/types/block";
import { BlockID } from "../../../tendermint/types/types";
import { GasInfo, Result, TxResponse } from "../../base/abci/v1beta1/abci";
import { PageRequest, PageResponse } from "../../base/query/v1beta1/pagination";
import { Tx } from "./tx";
import { BroadcastMode, OrderBy } from "../../../../types/cosmos/tx/v1beta1";
export const GetTxsEventRequest = {
    $type: "cosmos.tx.v1beta1.GetTxsEventRequest",
    encode(message, writer = new BinaryWriter()) {
        for (const v of message.events) {
            writer.uint32(10).string(v);
        }
        if (message.pagination !== undefined) {
            PageRequest.encode(message.pagination, writer.uint32(18).fork()).join();
        }
        if (message.order_by !== 0) {
            writer.uint32(24).int32(message.order_by);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseGetTxsEventRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.events.push(reader.string());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.pagination = PageRequest.decode(reader, reader.uint32());
                    continue;
                case 3:
                    if (tag !== 24) {
                        break;
                    }
                    message.order_by = reader.int32();
                    continue;
            }
            if ((tag & 7) === 4 || tag === 0) {
                break;
            }
            reader.skip(tag & 7);
        }
        return message;
    },
    fromJSON(object) {
        return {
            events: globalThis.Array.isArray(object?.events) ? object.events.map((e) => globalThis.String(e)) : [],
            pagination: isSet(object.pagination) ? PageRequest.fromJSON(object.pagination) : undefined,
            order_by: isSet(object.order_by) ? orderByFromJSON(object.order_by) : 0
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.events?.length) {
            obj.events = message.events;
        }
        if (message.pagination !== undefined) {
            obj.pagination = PageRequest.toJSON(message.pagination);
        }
        if (message.order_by !== 0) {
            obj.order_by = orderByToJSON(message.order_by);
        }
        return obj;
    },
    create(base) {
        return GetTxsEventRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGetTxsEventRequest();
        message.events = object.events?.map((e) => e) || [];
        message.pagination = object.pagination !== undefined && object.pagination !== null ? PageRequest.fromPartial(object.pagination) : undefined;
        message.order_by = object.order_by ?? 0;
        return message;
    }
};
export const GetTxsEventResponse = {
    $type: "cosmos.tx.v1beta1.GetTxsEventResponse",
    encode(message, writer = new BinaryWriter()) {
        for (const v of message.txs) {
            Tx.encode(v, writer.uint32(10).fork()).join();
        }
        for (const v of message.tx_responses) {
            TxResponse.encode(v, writer.uint32(18).fork()).join();
        }
        if (message.pagination !== undefined) {
            PageResponse.encode(message.pagination, writer.uint32(26).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseGetTxsEventResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.txs.push(Tx.decode(reader, reader.uint32()));
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.tx_responses.push(TxResponse.decode(reader, reader.uint32()));
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.pagination = PageResponse.decode(reader, reader.uint32());
                    continue;
            }
            if ((tag & 7) === 4 || tag === 0) {
                break;
            }
            reader.skip(tag & 7);
        }
        return message;
    },
    fromJSON(object) {
        return {
            txs: globalThis.Array.isArray(object?.txs) ? object.txs.map((e) => Tx.fromJSON(e)) : [],
            tx_responses: globalThis.Array.isArray(object?.tx_responses) ? object.tx_responses.map((e) => TxResponse.fromJSON(e)) : [],
            pagination: isSet(object.pagination) ? PageResponse.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.txs?.length) {
            obj.txs = message.txs.map((e) => Tx.toJSON(e));
        }
        if (message.tx_responses?.length) {
            obj.tx_responses = message.tx_responses.map((e) => TxResponse.toJSON(e));
        }
        if (message.pagination !== undefined) {
            obj.pagination = PageResponse.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return GetTxsEventResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGetTxsEventResponse();
        message.txs = object.txs?.map((e) => Tx.fromPartial(e)) || [];
        message.tx_responses = object.tx_responses?.map((e) => TxResponse.fromPartial(e)) || [];
        message.pagination = object.pagination !== undefined && object.pagination !== null ? PageResponse.fromPartial(object.pagination) : undefined;
        return message;
    }
};
export const BroadcastTxRequest = {
    $type: "cosmos.tx.v1beta1.BroadcastTxRequest",
    encode(message, writer = new BinaryWriter()) {
        if (message.tx_bytes.length !== 0) {
            writer.uint32(10).bytes(message.tx_bytes);
        }
        if (message.mode !== 0) {
            writer.uint32(16).int32(message.mode);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseBroadcastTxRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.tx_bytes = reader.bytes();
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.mode = reader.int32();
                    continue;
            }
            if ((tag & 7) === 4 || tag === 0) {
                break;
            }
            reader.skip(tag & 7);
        }
        return message;
    },
    fromJSON(object) {
        return {
            tx_bytes: isSet(object.tx_bytes) ? bytesFromBase64(object.tx_bytes) : new Uint8Array(0),
            mode: isSet(object.mode) ? broadcastModeFromJSON(object.mode) : 0
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.tx_bytes.length !== 0) {
            obj.tx_bytes = base64FromBytes(message.tx_bytes);
        }
        if (message.mode !== 0) {
            obj.mode = broadcastModeToJSON(message.mode);
        }
        return obj;
    },
    create(base) {
        return BroadcastTxRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseBroadcastTxRequest();
        message.tx_bytes = object.tx_bytes ?? new Uint8Array(0);
        message.mode = object.mode ?? 0;
        return message;
    }
};
export const BroadcastTxResponse = {
    $type: "cosmos.tx.v1beta1.BroadcastTxResponse",
    encode(message, writer = new BinaryWriter()) {
        if (message.tx_response !== undefined) {
            TxResponse.encode(message.tx_response, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseBroadcastTxResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.tx_response = TxResponse.decode(reader, reader.uint32());
                    continue;
            }
            if ((tag & 7) === 4 || tag === 0) {
                break;
            }
            reader.skip(tag & 7);
        }
        return message;
    },
    fromJSON(object) {
        return { tx_response: isSet(object.tx_response) ? TxResponse.fromJSON(object.tx_response) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.tx_response !== undefined) {
            obj.tx_response = TxResponse.toJSON(message.tx_response);
        }
        return obj;
    },
    create(base) {
        return BroadcastTxResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseBroadcastTxResponse();
        message.tx_response = object.tx_response !== undefined && object.tx_response !== null ? TxResponse.fromPartial(object.tx_response) : undefined;
        return message;
    }
};
export const SimulateRequest = {
    $type: "cosmos.tx.v1beta1.SimulateRequest",
    encode(message, writer = new BinaryWriter()) {
        if (message.tx !== undefined) {
            Tx.encode(message.tx, writer.uint32(10).fork()).join();
        }
        if (message.tx_bytes.length !== 0) {
            writer.uint32(18).bytes(message.tx_bytes);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseSimulateRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.tx = Tx.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.tx_bytes = reader.bytes();
                    continue;
            }
            if ((tag & 7) === 4 || tag === 0) {
                break;
            }
            reader.skip(tag & 7);
        }
        return message;
    },
    fromJSON(object) {
        return {
            tx: isSet(object.tx) ? Tx.fromJSON(object.tx) : undefined,
            tx_bytes: isSet(object.tx_bytes) ? bytesFromBase64(object.tx_bytes) : new Uint8Array(0)
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.tx !== undefined) {
            obj.tx = Tx.toJSON(message.tx);
        }
        if (message.tx_bytes.length !== 0) {
            obj.tx_bytes = base64FromBytes(message.tx_bytes);
        }
        return obj;
    },
    create(base) {
        return SimulateRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseSimulateRequest();
        message.tx = object.tx !== undefined && object.tx !== null ? Tx.fromPartial(object.tx) : undefined;
        message.tx_bytes = object.tx_bytes ?? new Uint8Array(0);
        return message;
    }
};
export const SimulateResponse = {
    $type: "cosmos.tx.v1beta1.SimulateResponse",
    encode(message, writer = new BinaryWriter()) {
        if (message.gas_info !== undefined) {
            GasInfo.encode(message.gas_info, writer.uint32(10).fork()).join();
        }
        if (message.result !== undefined) {
            Result.encode(message.result, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseSimulateResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.gas_info = GasInfo.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.result = Result.decode(reader, reader.uint32());
                    continue;
            }
            if ((tag & 7) === 4 || tag === 0) {
                break;
            }
            reader.skip(tag & 7);
        }
        return message;
    },
    fromJSON(object) {
        return {
            gas_info: isSet(object.gas_info) ? GasInfo.fromJSON(object.gas_info) : undefined,
            result: isSet(object.result) ? Result.fromJSON(object.result) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.gas_info !== undefined) {
            obj.gas_info = GasInfo.toJSON(message.gas_info);
        }
        if (message.result !== undefined) {
            obj.result = Result.toJSON(message.result);
        }
        return obj;
    },
    create(base) {
        return SimulateResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseSimulateResponse();
        message.gas_info = object.gas_info !== undefined && object.gas_info !== null ? GasInfo.fromPartial(object.gas_info) : undefined;
        message.result = object.result !== undefined && object.result !== null ? Result.fromPartial(object.result) : undefined;
        return message;
    }
};
export const GetTxRequest = {
    $type: "cosmos.tx.v1beta1.GetTxRequest",
    encode(message, writer = new BinaryWriter()) {
        if (message.hash !== "") {
            writer.uint32(10).string(message.hash);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseGetTxRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.hash = reader.string();
                    continue;
            }
            if ((tag & 7) === 4 || tag === 0) {
                break;
            }
            reader.skip(tag & 7);
        }
        return message;
    },
    fromJSON(object) {
        return { hash: isSet(object.hash) ? globalThis.String(object.hash) : "" };
    },
    toJSON(message) {
        const obj = {};
        if (message.hash !== "") {
            obj.hash = message.hash;
        }
        return obj;
    },
    create(base) {
        return GetTxRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGetTxRequest();
        message.hash = object.hash ?? "";
        return message;
    }
};
export const GetTxResponse = {
    $type: "cosmos.tx.v1beta1.GetTxResponse",
    encode(message, writer = new BinaryWriter()) {
        if (message.tx !== undefined) {
            Tx.encode(message.tx, writer.uint32(10).fork()).join();
        }
        if (message.tx_response !== undefined) {
            TxResponse.encode(message.tx_response, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseGetTxResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.tx = Tx.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.tx_response = TxResponse.decode(reader, reader.uint32());
                    continue;
            }
            if ((tag & 7) === 4 || tag === 0) {
                break;
            }
            reader.skip(tag & 7);
        }
        return message;
    },
    fromJSON(object) {
        return {
            tx: isSet(object.tx) ? Tx.fromJSON(object.tx) : undefined,
            tx_response: isSet(object.tx_response) ? TxResponse.fromJSON(object.tx_response) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.tx !== undefined) {
            obj.tx = Tx.toJSON(message.tx);
        }
        if (message.tx_response !== undefined) {
            obj.tx_response = TxResponse.toJSON(message.tx_response);
        }
        return obj;
    },
    create(base) {
        return GetTxResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGetTxResponse();
        message.tx = object.tx !== undefined && object.tx !== null ? Tx.fromPartial(object.tx) : undefined;
        message.tx_response = object.tx_response !== undefined && object.tx_response !== null ? TxResponse.fromPartial(object.tx_response) : undefined;
        return message;
    }
};
export const GetBlockWithTxsRequest = {
    $type: "cosmos.tx.v1beta1.GetBlockWithTxsRequest",
    encode(message, writer = new BinaryWriter()) {
        if (message.height !== 0) {
            writer.uint32(8).int64(message.height);
        }
        if (message.pagination !== undefined) {
            PageRequest.encode(message.pagination, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseGetBlockWithTxsRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.height = longToNumber(reader.int64());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.pagination = PageRequest.decode(reader, reader.uint32());
                    continue;
            }
            if ((tag & 7) === 4 || tag === 0) {
                break;
            }
            reader.skip(tag & 7);
        }
        return message;
    },
    fromJSON(object) {
        return {
            height: isSet(object.height) ? globalThis.Number(object.height) : 0,
            pagination: isSet(object.pagination) ? PageRequest.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.height !== 0) {
            obj.height = Math.round(message.height);
        }
        if (message.pagination !== undefined) {
            obj.pagination = PageRequest.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return GetBlockWithTxsRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGetBlockWithTxsRequest();
        message.height = object.height ?? 0;
        message.pagination = object.pagination !== undefined && object.pagination !== null ? PageRequest.fromPartial(object.pagination) : undefined;
        return message;
    }
};
export const GetBlockWithTxsResponse = {
    $type: "cosmos.tx.v1beta1.GetBlockWithTxsResponse",
    encode(message, writer = new BinaryWriter()) {
        for (const v of message.txs) {
            Tx.encode(v, writer.uint32(10).fork()).join();
        }
        if (message.block_id !== undefined) {
            BlockID.encode(message.block_id, writer.uint32(18).fork()).join();
        }
        if (message.block !== undefined) {
            Block.encode(message.block, writer.uint32(26).fork()).join();
        }
        if (message.pagination !== undefined) {
            PageResponse.encode(message.pagination, writer.uint32(34).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseGetBlockWithTxsResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.txs.push(Tx.decode(reader, reader.uint32()));
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.block_id = BlockID.decode(reader, reader.uint32());
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.block = Block.decode(reader, reader.uint32());
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.pagination = PageResponse.decode(reader, reader.uint32());
                    continue;
            }
            if ((tag & 7) === 4 || tag === 0) {
                break;
            }
            reader.skip(tag & 7);
        }
        return message;
    },
    fromJSON(object) {
        return {
            txs: globalThis.Array.isArray(object?.txs) ? object.txs.map((e) => Tx.fromJSON(e)) : [],
            block_id: isSet(object.block_id) ? BlockID.fromJSON(object.block_id) : undefined,
            block: isSet(object.block) ? Block.fromJSON(object.block) : undefined,
            pagination: isSet(object.pagination) ? PageResponse.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.txs?.length) {
            obj.txs = message.txs.map((e) => Tx.toJSON(e));
        }
        if (message.block_id !== undefined) {
            obj.block_id = BlockID.toJSON(message.block_id);
        }
        if (message.block !== undefined) {
            obj.block = Block.toJSON(message.block);
        }
        if (message.pagination !== undefined) {
            obj.pagination = PageResponse.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return GetBlockWithTxsResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGetBlockWithTxsResponse();
        message.txs = object.txs?.map((e) => Tx.fromPartial(e)) || [];
        message.block_id = object.block_id !== undefined && object.block_id !== null ? BlockID.fromPartial(object.block_id) : undefined;
        message.block = object.block !== undefined && object.block !== null ? Block.fromPartial(object.block) : undefined;
        message.pagination = object.pagination !== undefined && object.pagination !== null ? PageResponse.fromPartial(object.pagination) : undefined;
        return message;
    }
};
export function orderByFromJSON(object) {
    switch (object) {
        case 0:
        case "ORDER_BY_UNSPECIFIED":
            return OrderBy.ORDER_BY_UNSPECIFIED;
        case 1:
        case "ORDER_BY_ASC":
            return OrderBy.ORDER_BY_ASC;
        case 2:
        case "ORDER_BY_DESC":
            return OrderBy.ORDER_BY_DESC;
        case -1:
        case "UNRECOGNIZED":
        default:
            return OrderBy.UNRECOGNIZED;
    }
}
export function orderByToJSON(object) {
    switch (object) {
        case OrderBy.ORDER_BY_UNSPECIFIED:
            return "ORDER_BY_UNSPECIFIED";
        case OrderBy.ORDER_BY_ASC:
            return "ORDER_BY_ASC";
        case OrderBy.ORDER_BY_DESC:
            return "ORDER_BY_DESC";
        case OrderBy.UNRECOGNIZED:
        default:
            return "UNRECOGNIZED";
    }
}
export function broadcastModeFromJSON(object) {
    switch (object) {
        case 0:
        case "BROADCAST_MODE_UNSPECIFIED":
            return BroadcastMode.BROADCAST_MODE_UNSPECIFIED;
        case 1:
        case "BROADCAST_MODE_BLOCK":
            return BroadcastMode.BROADCAST_MODE_BLOCK;
        case 2:
        case "BROADCAST_MODE_SYNC":
            return BroadcastMode.BROADCAST_MODE_SYNC;
        case 3:
        case "BROADCAST_MODE_ASYNC":
            return BroadcastMode.BROADCAST_MODE_ASYNC;
        case -1:
        case "UNRECOGNIZED":
        default:
            return BroadcastMode.UNRECOGNIZED;
    }
}
export function broadcastModeToJSON(object) {
    switch (object) {
        case BroadcastMode.BROADCAST_MODE_UNSPECIFIED:
            return "BROADCAST_MODE_UNSPECIFIED";
        case BroadcastMode.BROADCAST_MODE_BLOCK:
            return "BROADCAST_MODE_BLOCK";
        case BroadcastMode.BROADCAST_MODE_SYNC:
            return "BROADCAST_MODE_SYNC";
        case BroadcastMode.BROADCAST_MODE_ASYNC:
            return "BROADCAST_MODE_ASYNC";
        case BroadcastMode.UNRECOGNIZED:
        default:
            return "UNRECOGNIZED";
    }
}
function createBaseGetTxsEventRequest() {
    return { events: [], pagination: undefined, order_by: 0 };
}
function createBaseGetTxsEventResponse() {
    return { txs: [], tx_responses: [], pagination: undefined };
}
function createBaseBroadcastTxRequest() {
    return { tx_bytes: new Uint8Array(0), mode: 0 };
}
function createBaseBroadcastTxResponse() {
    return { tx_response: undefined };
}
function createBaseSimulateRequest() {
    return { tx: undefined, tx_bytes: new Uint8Array(0) };
}
function createBaseSimulateResponse() {
    return { gas_info: undefined, result: undefined };
}
function createBaseGetTxRequest() {
    return { hash: "" };
}
function createBaseGetTxResponse() {
    return { tx: undefined, tx_response: undefined };
}
function createBaseGetBlockWithTxsRequest() {
    return { height: 0, pagination: undefined };
}
function createBaseGetBlockWithTxsResponse() {
    return { txs: [], block_id: undefined, block: undefined, pagination: undefined };
}
function bytesFromBase64(b64) {
    if (globalThis.Buffer) {
        return Uint8Array.from(globalThis.Buffer.from(b64, "base64"));
    }
    else {
        const bin = globalThis.atob(b64);
        const arr = new Uint8Array(bin.length);
        for (let i = 0; i < bin.length; ++i) {
            arr[i] = bin.charCodeAt(i);
        }
        return arr;
    }
}
function base64FromBytes(arr) {
    if (globalThis.Buffer) {
        return globalThis.Buffer.from(arr).toString("base64");
    }
    else {
        const bin = [];
        arr.forEach((byte) => {
            bin.push(globalThis.String.fromCharCode(byte));
        });
        return globalThis.btoa(bin.join(""));
    }
}
function longToNumber(int64) {
    const num = globalThis.Number(int64.toString());
    if (num > globalThis.Number.MAX_SAFE_INTEGER) {
        throw new globalThis.Error("Value is larger than Number.MAX_SAFE_INTEGER");
    }
    if (num < globalThis.Number.MIN_SAFE_INTEGER) {
        throw new globalThis.Error("Value is smaller than Number.MIN_SAFE_INTEGER");
    }
    return num;
}
function isSet(value) {
    return value !== null && value !== undefined;
}
export const registry = [
    ["/cosmos.tx.v1beta1.GetTxsEventRequest", GetTxsEventRequest],
    ["/cosmos.tx.v1beta1.GetTxsEventResponse", GetTxsEventResponse],
    ["/cosmos.tx.v1beta1.BroadcastTxRequest", BroadcastTxRequest],
    ["/cosmos.tx.v1beta1.BroadcastTxResponse", BroadcastTxResponse],
    ["/cosmos.tx.v1beta1.SimulateRequest", SimulateRequest],
    ["/cosmos.tx.v1beta1.SimulateResponse", SimulateResponse],
    ["/cosmos.tx.v1beta1.GetTxRequest", GetTxRequest],
    ["/cosmos.tx.v1beta1.GetTxResponse", GetTxResponse],
    ["/cosmos.tx.v1beta1.GetBlockWithTxsRequest", GetBlockWithTxsRequest],
    ["/cosmos.tx.v1beta1.GetBlockWithTxsResponse", GetBlockWithTxsResponse]
];
export const aminoConverters = {
    "/cosmos.tx.v1beta1.GetTxsEventRequest": {
        aminoType: "cosmos-sdk/GetTxsEventRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.tx.v1beta1.GetTxsEventResponse": {
        aminoType: "cosmos-sdk/GetTxsEventResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.tx.v1beta1.BroadcastTxRequest": {
        aminoType: "cosmos-sdk/BroadcastTxRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.tx.v1beta1.BroadcastTxResponse": {
        aminoType: "cosmos-sdk/BroadcastTxResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.tx.v1beta1.SimulateRequest": {
        aminoType: "cosmos-sdk/SimulateRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.tx.v1beta1.SimulateResponse": {
        aminoType: "cosmos-sdk/SimulateResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.tx.v1beta1.GetTxRequest": {
        aminoType: "cosmos-sdk/GetTxRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.tx.v1beta1.GetTxResponse": {
        aminoType: "cosmos-sdk/GetTxResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.tx.v1beta1.GetBlockWithTxsRequest": {
        aminoType: "cosmos-sdk/GetBlockWithTxsRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.tx.v1beta1.GetBlockWithTxsResponse": {
        aminoType: "cosmos-sdk/GetBlockWithTxsResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
