"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.GetBlockWithTxsResponse = exports.GetBlockWithTxsRequest = exports.GetTxResponse = exports.GetTxRequest = exports.SimulateResponse = exports.SimulateRequest = exports.BroadcastTxResponse = exports.BroadcastTxRequest = exports.GetTxsEventResponse = exports.GetTxsEventRequest = void 0;
exports.orderByFromJSON = orderByFromJSON;
exports.orderByToJSON = orderByToJSON;
exports.broadcastModeFromJSON = broadcastModeFromJSON;
exports.broadcastModeToJSON = broadcastModeToJSON;
const wire_1 = require("@bufbuild/protobuf/wire");
const block_1 = require("../../../tendermint/types/block");
const types_1 = require("../../../tendermint/types/types");
const abci_1 = require("../../base/abci/v1beta1/abci");
const pagination_1 = require("../../base/query/v1beta1/pagination");
const tx_1 = require("./tx");
const v1beta1_1 = require("../../../../types/cosmos/tx/v1beta1");
exports.GetTxsEventRequest = {
    $type: "cosmos.tx.v1beta1.GetTxsEventRequest",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.events) {
            writer.uint32(10).string(v);
        }
        if (message.pagination !== undefined) {
            pagination_1.PageRequest.encode(message.pagination, writer.uint32(18).fork()).join();
        }
        if (message.order_by !== 0) {
            writer.uint32(24).int32(message.order_by);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
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
                    message.pagination = pagination_1.PageRequest.decode(reader, reader.uint32());
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
            pagination: isSet(object.pagination) ? pagination_1.PageRequest.fromJSON(object.pagination) : undefined,
            order_by: isSet(object.order_by) ? orderByFromJSON(object.order_by) : 0
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.events?.length) {
            obj.events = message.events;
        }
        if (message.pagination !== undefined) {
            obj.pagination = pagination_1.PageRequest.toJSON(message.pagination);
        }
        if (message.order_by !== 0) {
            obj.order_by = orderByToJSON(message.order_by);
        }
        return obj;
    },
    create(base) {
        return exports.GetTxsEventRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGetTxsEventRequest();
        message.events = object.events?.map((e) => e) || [];
        message.pagination = object.pagination !== undefined && object.pagination !== null ? pagination_1.PageRequest.fromPartial(object.pagination) : undefined;
        message.order_by = object.order_by ?? 0;
        return message;
    }
};
exports.GetTxsEventResponse = {
    $type: "cosmos.tx.v1beta1.GetTxsEventResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.txs) {
            tx_1.Tx.encode(v, writer.uint32(10).fork()).join();
        }
        for (const v of message.tx_responses) {
            abci_1.TxResponse.encode(v, writer.uint32(18).fork()).join();
        }
        if (message.pagination !== undefined) {
            pagination_1.PageResponse.encode(message.pagination, writer.uint32(26).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseGetTxsEventResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.txs.push(tx_1.Tx.decode(reader, reader.uint32()));
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.tx_responses.push(abci_1.TxResponse.decode(reader, reader.uint32()));
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.pagination = pagination_1.PageResponse.decode(reader, reader.uint32());
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
            txs: globalThis.Array.isArray(object?.txs) ? object.txs.map((e) => tx_1.Tx.fromJSON(e)) : [],
            tx_responses: globalThis.Array.isArray(object?.tx_responses) ? object.tx_responses.map((e) => abci_1.TxResponse.fromJSON(e)) : [],
            pagination: isSet(object.pagination) ? pagination_1.PageResponse.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.txs?.length) {
            obj.txs = message.txs.map((e) => tx_1.Tx.toJSON(e));
        }
        if (message.tx_responses?.length) {
            obj.tx_responses = message.tx_responses.map((e) => abci_1.TxResponse.toJSON(e));
        }
        if (message.pagination !== undefined) {
            obj.pagination = pagination_1.PageResponse.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return exports.GetTxsEventResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGetTxsEventResponse();
        message.txs = object.txs?.map((e) => tx_1.Tx.fromPartial(e)) || [];
        message.tx_responses = object.tx_responses?.map((e) => abci_1.TxResponse.fromPartial(e)) || [];
        message.pagination = object.pagination !== undefined && object.pagination !== null ? pagination_1.PageResponse.fromPartial(object.pagination) : undefined;
        return message;
    }
};
exports.BroadcastTxRequest = {
    $type: "cosmos.tx.v1beta1.BroadcastTxRequest",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.tx_bytes.length !== 0) {
            writer.uint32(10).bytes(message.tx_bytes);
        }
        if (message.mode !== 0) {
            writer.uint32(16).int32(message.mode);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
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
        return exports.BroadcastTxRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseBroadcastTxRequest();
        message.tx_bytes = object.tx_bytes ?? new Uint8Array(0);
        message.mode = object.mode ?? 0;
        return message;
    }
};
exports.BroadcastTxResponse = {
    $type: "cosmos.tx.v1beta1.BroadcastTxResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.tx_response !== undefined) {
            abci_1.TxResponse.encode(message.tx_response, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseBroadcastTxResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.tx_response = abci_1.TxResponse.decode(reader, reader.uint32());
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
        return { tx_response: isSet(object.tx_response) ? abci_1.TxResponse.fromJSON(object.tx_response) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.tx_response !== undefined) {
            obj.tx_response = abci_1.TxResponse.toJSON(message.tx_response);
        }
        return obj;
    },
    create(base) {
        return exports.BroadcastTxResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseBroadcastTxResponse();
        message.tx_response = object.tx_response !== undefined && object.tx_response !== null ? abci_1.TxResponse.fromPartial(object.tx_response) : undefined;
        return message;
    }
};
exports.SimulateRequest = {
    $type: "cosmos.tx.v1beta1.SimulateRequest",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.tx !== undefined) {
            tx_1.Tx.encode(message.tx, writer.uint32(10).fork()).join();
        }
        if (message.tx_bytes.length !== 0) {
            writer.uint32(18).bytes(message.tx_bytes);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseSimulateRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.tx = tx_1.Tx.decode(reader, reader.uint32());
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
            tx: isSet(object.tx) ? tx_1.Tx.fromJSON(object.tx) : undefined,
            tx_bytes: isSet(object.tx_bytes) ? bytesFromBase64(object.tx_bytes) : new Uint8Array(0)
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.tx !== undefined) {
            obj.tx = tx_1.Tx.toJSON(message.tx);
        }
        if (message.tx_bytes.length !== 0) {
            obj.tx_bytes = base64FromBytes(message.tx_bytes);
        }
        return obj;
    },
    create(base) {
        return exports.SimulateRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseSimulateRequest();
        message.tx = object.tx !== undefined && object.tx !== null ? tx_1.Tx.fromPartial(object.tx) : undefined;
        message.tx_bytes = object.tx_bytes ?? new Uint8Array(0);
        return message;
    }
};
exports.SimulateResponse = {
    $type: "cosmos.tx.v1beta1.SimulateResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.gas_info !== undefined) {
            abci_1.GasInfo.encode(message.gas_info, writer.uint32(10).fork()).join();
        }
        if (message.result !== undefined) {
            abci_1.Result.encode(message.result, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseSimulateResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.gas_info = abci_1.GasInfo.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.result = abci_1.Result.decode(reader, reader.uint32());
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
            gas_info: isSet(object.gas_info) ? abci_1.GasInfo.fromJSON(object.gas_info) : undefined,
            result: isSet(object.result) ? abci_1.Result.fromJSON(object.result) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.gas_info !== undefined) {
            obj.gas_info = abci_1.GasInfo.toJSON(message.gas_info);
        }
        if (message.result !== undefined) {
            obj.result = abci_1.Result.toJSON(message.result);
        }
        return obj;
    },
    create(base) {
        return exports.SimulateResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseSimulateResponse();
        message.gas_info = object.gas_info !== undefined && object.gas_info !== null ? abci_1.GasInfo.fromPartial(object.gas_info) : undefined;
        message.result = object.result !== undefined && object.result !== null ? abci_1.Result.fromPartial(object.result) : undefined;
        return message;
    }
};
exports.GetTxRequest = {
    $type: "cosmos.tx.v1beta1.GetTxRequest",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.hash !== "") {
            writer.uint32(10).string(message.hash);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
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
        return exports.GetTxRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGetTxRequest();
        message.hash = object.hash ?? "";
        return message;
    }
};
exports.GetTxResponse = {
    $type: "cosmos.tx.v1beta1.GetTxResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.tx !== undefined) {
            tx_1.Tx.encode(message.tx, writer.uint32(10).fork()).join();
        }
        if (message.tx_response !== undefined) {
            abci_1.TxResponse.encode(message.tx_response, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseGetTxResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.tx = tx_1.Tx.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.tx_response = abci_1.TxResponse.decode(reader, reader.uint32());
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
            tx: isSet(object.tx) ? tx_1.Tx.fromJSON(object.tx) : undefined,
            tx_response: isSet(object.tx_response) ? abci_1.TxResponse.fromJSON(object.tx_response) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.tx !== undefined) {
            obj.tx = tx_1.Tx.toJSON(message.tx);
        }
        if (message.tx_response !== undefined) {
            obj.tx_response = abci_1.TxResponse.toJSON(message.tx_response);
        }
        return obj;
    },
    create(base) {
        return exports.GetTxResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGetTxResponse();
        message.tx = object.tx !== undefined && object.tx !== null ? tx_1.Tx.fromPartial(object.tx) : undefined;
        message.tx_response = object.tx_response !== undefined && object.tx_response !== null ? abci_1.TxResponse.fromPartial(object.tx_response) : undefined;
        return message;
    }
};
exports.GetBlockWithTxsRequest = {
    $type: "cosmos.tx.v1beta1.GetBlockWithTxsRequest",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.height !== 0) {
            writer.uint32(8).int64(message.height);
        }
        if (message.pagination !== undefined) {
            pagination_1.PageRequest.encode(message.pagination, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
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
                    message.pagination = pagination_1.PageRequest.decode(reader, reader.uint32());
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
            pagination: isSet(object.pagination) ? pagination_1.PageRequest.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.height !== 0) {
            obj.height = Math.round(message.height);
        }
        if (message.pagination !== undefined) {
            obj.pagination = pagination_1.PageRequest.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return exports.GetBlockWithTxsRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGetBlockWithTxsRequest();
        message.height = object.height ?? 0;
        message.pagination = object.pagination !== undefined && object.pagination !== null ? pagination_1.PageRequest.fromPartial(object.pagination) : undefined;
        return message;
    }
};
exports.GetBlockWithTxsResponse = {
    $type: "cosmos.tx.v1beta1.GetBlockWithTxsResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.txs) {
            tx_1.Tx.encode(v, writer.uint32(10).fork()).join();
        }
        if (message.block_id !== undefined) {
            types_1.BlockID.encode(message.block_id, writer.uint32(18).fork()).join();
        }
        if (message.block !== undefined) {
            block_1.Block.encode(message.block, writer.uint32(26).fork()).join();
        }
        if (message.pagination !== undefined) {
            pagination_1.PageResponse.encode(message.pagination, writer.uint32(34).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseGetBlockWithTxsResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.txs.push(tx_1.Tx.decode(reader, reader.uint32()));
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.block_id = types_1.BlockID.decode(reader, reader.uint32());
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.block = block_1.Block.decode(reader, reader.uint32());
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.pagination = pagination_1.PageResponse.decode(reader, reader.uint32());
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
            txs: globalThis.Array.isArray(object?.txs) ? object.txs.map((e) => tx_1.Tx.fromJSON(e)) : [],
            block_id: isSet(object.block_id) ? types_1.BlockID.fromJSON(object.block_id) : undefined,
            block: isSet(object.block) ? block_1.Block.fromJSON(object.block) : undefined,
            pagination: isSet(object.pagination) ? pagination_1.PageResponse.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.txs?.length) {
            obj.txs = message.txs.map((e) => tx_1.Tx.toJSON(e));
        }
        if (message.block_id !== undefined) {
            obj.block_id = types_1.BlockID.toJSON(message.block_id);
        }
        if (message.block !== undefined) {
            obj.block = block_1.Block.toJSON(message.block);
        }
        if (message.pagination !== undefined) {
            obj.pagination = pagination_1.PageResponse.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return exports.GetBlockWithTxsResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGetBlockWithTxsResponse();
        message.txs = object.txs?.map((e) => tx_1.Tx.fromPartial(e)) || [];
        message.block_id = object.block_id !== undefined && object.block_id !== null ? types_1.BlockID.fromPartial(object.block_id) : undefined;
        message.block = object.block !== undefined && object.block !== null ? block_1.Block.fromPartial(object.block) : undefined;
        message.pagination = object.pagination !== undefined && object.pagination !== null ? pagination_1.PageResponse.fromPartial(object.pagination) : undefined;
        return message;
    }
};
function orderByFromJSON(object) {
    switch (object) {
        case 0:
        case "ORDER_BY_UNSPECIFIED":
            return v1beta1_1.OrderBy.ORDER_BY_UNSPECIFIED;
        case 1:
        case "ORDER_BY_ASC":
            return v1beta1_1.OrderBy.ORDER_BY_ASC;
        case 2:
        case "ORDER_BY_DESC":
            return v1beta1_1.OrderBy.ORDER_BY_DESC;
        case -1:
        case "UNRECOGNIZED":
        default:
            return v1beta1_1.OrderBy.UNRECOGNIZED;
    }
}
function orderByToJSON(object) {
    switch (object) {
        case v1beta1_1.OrderBy.ORDER_BY_UNSPECIFIED:
            return "ORDER_BY_UNSPECIFIED";
        case v1beta1_1.OrderBy.ORDER_BY_ASC:
            return "ORDER_BY_ASC";
        case v1beta1_1.OrderBy.ORDER_BY_DESC:
            return "ORDER_BY_DESC";
        case v1beta1_1.OrderBy.UNRECOGNIZED:
        default:
            return "UNRECOGNIZED";
    }
}
function broadcastModeFromJSON(object) {
    switch (object) {
        case 0:
        case "BROADCAST_MODE_UNSPECIFIED":
            return v1beta1_1.BroadcastMode.BROADCAST_MODE_UNSPECIFIED;
        case 1:
        case "BROADCAST_MODE_BLOCK":
            return v1beta1_1.BroadcastMode.BROADCAST_MODE_BLOCK;
        case 2:
        case "BROADCAST_MODE_SYNC":
            return v1beta1_1.BroadcastMode.BROADCAST_MODE_SYNC;
        case 3:
        case "BROADCAST_MODE_ASYNC":
            return v1beta1_1.BroadcastMode.BROADCAST_MODE_ASYNC;
        case -1:
        case "UNRECOGNIZED":
        default:
            return v1beta1_1.BroadcastMode.UNRECOGNIZED;
    }
}
function broadcastModeToJSON(object) {
    switch (object) {
        case v1beta1_1.BroadcastMode.BROADCAST_MODE_UNSPECIFIED:
            return "BROADCAST_MODE_UNSPECIFIED";
        case v1beta1_1.BroadcastMode.BROADCAST_MODE_BLOCK:
            return "BROADCAST_MODE_BLOCK";
        case v1beta1_1.BroadcastMode.BROADCAST_MODE_SYNC:
            return "BROADCAST_MODE_SYNC";
        case v1beta1_1.BroadcastMode.BROADCAST_MODE_ASYNC:
            return "BROADCAST_MODE_ASYNC";
        case v1beta1_1.BroadcastMode.UNRECOGNIZED:
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
exports.registry = [
    ["/cosmos.tx.v1beta1.GetTxsEventRequest", exports.GetTxsEventRequest],
    ["/cosmos.tx.v1beta1.GetTxsEventResponse", exports.GetTxsEventResponse],
    ["/cosmos.tx.v1beta1.BroadcastTxRequest", exports.BroadcastTxRequest],
    ["/cosmos.tx.v1beta1.BroadcastTxResponse", exports.BroadcastTxResponse],
    ["/cosmos.tx.v1beta1.SimulateRequest", exports.SimulateRequest],
    ["/cosmos.tx.v1beta1.SimulateResponse", exports.SimulateResponse],
    ["/cosmos.tx.v1beta1.GetTxRequest", exports.GetTxRequest],
    ["/cosmos.tx.v1beta1.GetTxResponse", exports.GetTxResponse],
    ["/cosmos.tx.v1beta1.GetBlockWithTxsRequest", exports.GetBlockWithTxsRequest],
    ["/cosmos.tx.v1beta1.GetBlockWithTxsResponse", exports.GetBlockWithTxsResponse]
];
exports.aminoConverters = {
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
