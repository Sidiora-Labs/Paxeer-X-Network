import { BinaryReader, BinaryWriter } from "@bufbuild/protobuf/wire";
import { Any } from "../../../../google/protobuf/any";
import { Event } from "../../../../tendermint/abci/types";
export const TxResponse = {
    $type: "cosmos.base.abci.v1beta1.TxResponse",
    encode(message, writer = new BinaryWriter()) {
        if (message.height !== 0) {
            writer.uint32(8).int64(message.height);
        }
        if (message.txhash !== "") {
            writer.uint32(18).string(message.txhash);
        }
        if (message.codespace !== "") {
            writer.uint32(26).string(message.codespace);
        }
        if (message.code !== 0) {
            writer.uint32(32).uint32(message.code);
        }
        if (message.data !== "") {
            writer.uint32(42).string(message.data);
        }
        if (message.raw_log !== "") {
            writer.uint32(50).string(message.raw_log);
        }
        for (const v of message.logs) {
            ABCIMessageLog.encode(v, writer.uint32(58).fork()).join();
        }
        if (message.info !== "") {
            writer.uint32(66).string(message.info);
        }
        if (message.gas_wanted !== 0) {
            writer.uint32(72).int64(message.gas_wanted);
        }
        if (message.gas_used !== 0) {
            writer.uint32(80).int64(message.gas_used);
        }
        if (message.tx !== undefined) {
            Any.encode(message.tx, writer.uint32(90).fork()).join();
        }
        if (message.timestamp !== "") {
            writer.uint32(98).string(message.timestamp);
        }
        for (const v of message.events) {
            Event.encode(v, writer.uint32(106).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseTxResponse();
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
                    message.txhash = reader.string();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.codespace = reader.string();
                    continue;
                case 4:
                    if (tag !== 32) {
                        break;
                    }
                    message.code = reader.uint32();
                    continue;
                case 5:
                    if (tag !== 42) {
                        break;
                    }
                    message.data = reader.string();
                    continue;
                case 6:
                    if (tag !== 50) {
                        break;
                    }
                    message.raw_log = reader.string();
                    continue;
                case 7:
                    if (tag !== 58) {
                        break;
                    }
                    message.logs.push(ABCIMessageLog.decode(reader, reader.uint32()));
                    continue;
                case 8:
                    if (tag !== 66) {
                        break;
                    }
                    message.info = reader.string();
                    continue;
                case 9:
                    if (tag !== 72) {
                        break;
                    }
                    message.gas_wanted = longToNumber(reader.int64());
                    continue;
                case 10:
                    if (tag !== 80) {
                        break;
                    }
                    message.gas_used = longToNumber(reader.int64());
                    continue;
                case 11:
                    if (tag !== 90) {
                        break;
                    }
                    message.tx = Any.decode(reader, reader.uint32());
                    continue;
                case 12:
                    if (tag !== 98) {
                        break;
                    }
                    message.timestamp = reader.string();
                    continue;
                case 13:
                    if (tag !== 106) {
                        break;
                    }
                    message.events.push(Event.decode(reader, reader.uint32()));
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
            txhash: isSet(object.txhash) ? globalThis.String(object.txhash) : "",
            codespace: isSet(object.codespace) ? globalThis.String(object.codespace) : "",
            code: isSet(object.code) ? globalThis.Number(object.code) : 0,
            data: isSet(object.data) ? globalThis.String(object.data) : "",
            raw_log: isSet(object.raw_log) ? globalThis.String(object.raw_log) : "",
            logs: globalThis.Array.isArray(object?.logs) ? object.logs.map((e) => ABCIMessageLog.fromJSON(e)) : [],
            info: isSet(object.info) ? globalThis.String(object.info) : "",
            gas_wanted: isSet(object.gas_wanted) ? globalThis.Number(object.gas_wanted) : 0,
            gas_used: isSet(object.gas_used) ? globalThis.Number(object.gas_used) : 0,
            tx: isSet(object.tx) ? Any.fromJSON(object.tx) : undefined,
            timestamp: isSet(object.timestamp) ? globalThis.String(object.timestamp) : "",
            events: globalThis.Array.isArray(object?.events) ? object.events.map((e) => Event.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.height !== 0) {
            obj.height = Math.round(message.height);
        }
        if (message.txhash !== "") {
            obj.txhash = message.txhash;
        }
        if (message.codespace !== "") {
            obj.codespace = message.codespace;
        }
        if (message.code !== 0) {
            obj.code = Math.round(message.code);
        }
        if (message.data !== "") {
            obj.data = message.data;
        }
        if (message.raw_log !== "") {
            obj.raw_log = message.raw_log;
        }
        if (message.logs?.length) {
            obj.logs = message.logs.map((e) => ABCIMessageLog.toJSON(e));
        }
        if (message.info !== "") {
            obj.info = message.info;
        }
        if (message.gas_wanted !== 0) {
            obj.gas_wanted = Math.round(message.gas_wanted);
        }
        if (message.gas_used !== 0) {
            obj.gas_used = Math.round(message.gas_used);
        }
        if (message.tx !== undefined) {
            obj.tx = Any.toJSON(message.tx);
        }
        if (message.timestamp !== "") {
            obj.timestamp = message.timestamp;
        }
        if (message.events?.length) {
            obj.events = message.events.map((e) => Event.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return TxResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseTxResponse();
        message.height = object.height ?? 0;
        message.txhash = object.txhash ?? "";
        message.codespace = object.codespace ?? "";
        message.code = object.code ?? 0;
        message.data = object.data ?? "";
        message.raw_log = object.raw_log ?? "";
        message.logs = object.logs?.map((e) => ABCIMessageLog.fromPartial(e)) || [];
        message.info = object.info ?? "";
        message.gas_wanted = object.gas_wanted ?? 0;
        message.gas_used = object.gas_used ?? 0;
        message.tx = object.tx !== undefined && object.tx !== null ? Any.fromPartial(object.tx) : undefined;
        message.timestamp = object.timestamp ?? "";
        message.events = object.events?.map((e) => Event.fromPartial(e)) || [];
        return message;
    }
};
export const ABCIMessageLog = {
    $type: "cosmos.base.abci.v1beta1.ABCIMessageLog",
    encode(message, writer = new BinaryWriter()) {
        if (message.msg_index !== 0) {
            writer.uint32(8).uint32(message.msg_index);
        }
        if (message.log !== "") {
            writer.uint32(18).string(message.log);
        }
        for (const v of message.events) {
            StringEvent.encode(v, writer.uint32(26).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseABCIMessageLog();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.msg_index = reader.uint32();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.log = reader.string();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.events.push(StringEvent.decode(reader, reader.uint32()));
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
            msg_index: isSet(object.msg_index) ? globalThis.Number(object.msg_index) : 0,
            log: isSet(object.log) ? globalThis.String(object.log) : "",
            events: globalThis.Array.isArray(object?.events) ? object.events.map((e) => StringEvent.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.msg_index !== 0) {
            obj.msg_index = Math.round(message.msg_index);
        }
        if (message.log !== "") {
            obj.log = message.log;
        }
        if (message.events?.length) {
            obj.events = message.events.map((e) => StringEvent.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return ABCIMessageLog.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseABCIMessageLog();
        message.msg_index = object.msg_index ?? 0;
        message.log = object.log ?? "";
        message.events = object.events?.map((e) => StringEvent.fromPartial(e)) || [];
        return message;
    }
};
export const StringEvent = {
    $type: "cosmos.base.abci.v1beta1.StringEvent",
    encode(message, writer = new BinaryWriter()) {
        if (message.type !== "") {
            writer.uint32(10).string(message.type);
        }
        for (const v of message.attributes) {
            Attribute.encode(v, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseStringEvent();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.type = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.attributes.push(Attribute.decode(reader, reader.uint32()));
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
            type: isSet(object.type) ? globalThis.String(object.type) : "",
            attributes: globalThis.Array.isArray(object?.attributes) ? object.attributes.map((e) => Attribute.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.type !== "") {
            obj.type = message.type;
        }
        if (message.attributes?.length) {
            obj.attributes = message.attributes.map((e) => Attribute.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return StringEvent.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseStringEvent();
        message.type = object.type ?? "";
        message.attributes = object.attributes?.map((e) => Attribute.fromPartial(e)) || [];
        return message;
    }
};
export const Attribute = {
    $type: "cosmos.base.abci.v1beta1.Attribute",
    encode(message, writer = new BinaryWriter()) {
        if (message.key !== "") {
            writer.uint32(10).string(message.key);
        }
        if (message.value !== "") {
            writer.uint32(18).string(message.value);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseAttribute();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.key = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.value = reader.string();
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
            key: isSet(object.key) ? globalThis.String(object.key) : "",
            value: isSet(object.value) ? globalThis.String(object.value) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.key !== "") {
            obj.key = message.key;
        }
        if (message.value !== "") {
            obj.value = message.value;
        }
        return obj;
    },
    create(base) {
        return Attribute.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseAttribute();
        message.key = object.key ?? "";
        message.value = object.value ?? "";
        return message;
    }
};
export const GasInfo = {
    $type: "cosmos.base.abci.v1beta1.GasInfo",
    encode(message, writer = new BinaryWriter()) {
        if (message.gas_wanted !== 0) {
            writer.uint32(8).uint64(message.gas_wanted);
        }
        if (message.gas_used !== 0) {
            writer.uint32(16).uint64(message.gas_used);
        }
        if (message.gas_estimate !== 0) {
            writer.uint32(24).uint64(message.gas_estimate);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseGasInfo();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.gas_wanted = longToNumber(reader.uint64());
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.gas_used = longToNumber(reader.uint64());
                    continue;
                case 3:
                    if (tag !== 24) {
                        break;
                    }
                    message.gas_estimate = longToNumber(reader.uint64());
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
            gas_wanted: isSet(object.gas_wanted) ? globalThis.Number(object.gas_wanted) : 0,
            gas_used: isSet(object.gas_used) ? globalThis.Number(object.gas_used) : 0,
            gas_estimate: isSet(object.gas_estimate) ? globalThis.Number(object.gas_estimate) : 0
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.gas_wanted !== 0) {
            obj.gas_wanted = Math.round(message.gas_wanted);
        }
        if (message.gas_used !== 0) {
            obj.gas_used = Math.round(message.gas_used);
        }
        if (message.gas_estimate !== 0) {
            obj.gas_estimate = Math.round(message.gas_estimate);
        }
        return obj;
    },
    create(base) {
        return GasInfo.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGasInfo();
        message.gas_wanted = object.gas_wanted ?? 0;
        message.gas_used = object.gas_used ?? 0;
        message.gas_estimate = object.gas_estimate ?? 0;
        return message;
    }
};
export const Result = {
    $type: "cosmos.base.abci.v1beta1.Result",
    encode(message, writer = new BinaryWriter()) {
        if (message.data.length !== 0) {
            writer.uint32(10).bytes(message.data);
        }
        if (message.log !== "") {
            writer.uint32(18).string(message.log);
        }
        for (const v of message.events) {
            Event.encode(v, writer.uint32(26).fork()).join();
        }
        if (message.evmError !== "") {
            writer.uint32(34).string(message.evmError);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseResult();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.data = reader.bytes();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.log = reader.string();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.events.push(Event.decode(reader, reader.uint32()));
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.evmError = reader.string();
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
            data: isSet(object.data) ? bytesFromBase64(object.data) : new Uint8Array(0),
            log: isSet(object.log) ? globalThis.String(object.log) : "",
            events: globalThis.Array.isArray(object?.events) ? object.events.map((e) => Event.fromJSON(e)) : [],
            evmError: isSet(object.evmError) ? globalThis.String(object.evmError) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.data.length !== 0) {
            obj.data = base64FromBytes(message.data);
        }
        if (message.log !== "") {
            obj.log = message.log;
        }
        if (message.events?.length) {
            obj.events = message.events.map((e) => Event.toJSON(e));
        }
        if (message.evmError !== "") {
            obj.evmError = message.evmError;
        }
        return obj;
    },
    create(base) {
        return Result.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseResult();
        message.data = object.data ?? new Uint8Array(0);
        message.log = object.log ?? "";
        message.events = object.events?.map((e) => Event.fromPartial(e)) || [];
        message.evmError = object.evmError ?? "";
        return message;
    }
};
export const SimulationResponse = {
    $type: "cosmos.base.abci.v1beta1.SimulationResponse",
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
        const message = createBaseSimulationResponse();
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
        return SimulationResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseSimulationResponse();
        message.gas_info = object.gas_info !== undefined && object.gas_info !== null ? GasInfo.fromPartial(object.gas_info) : undefined;
        message.result = object.result !== undefined && object.result !== null ? Result.fromPartial(object.result) : undefined;
        return message;
    }
};
export const MsgData = {
    $type: "cosmos.base.abci.v1beta1.MsgData",
    encode(message, writer = new BinaryWriter()) {
        if (message.msg_type !== "") {
            writer.uint32(10).string(message.msg_type);
        }
        if (message.data.length !== 0) {
            writer.uint32(18).bytes(message.data);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgData();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.msg_type = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.data = reader.bytes();
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
            msg_type: isSet(object.msg_type) ? globalThis.String(object.msg_type) : "",
            data: isSet(object.data) ? bytesFromBase64(object.data) : new Uint8Array(0)
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.msg_type !== "") {
            obj.msg_type = message.msg_type;
        }
        if (message.data.length !== 0) {
            obj.data = base64FromBytes(message.data);
        }
        return obj;
    },
    create(base) {
        return MsgData.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgData();
        message.msg_type = object.msg_type ?? "";
        message.data = object.data ?? new Uint8Array(0);
        return message;
    }
};
export const TxMsgData = {
    $type: "cosmos.base.abci.v1beta1.TxMsgData",
    encode(message, writer = new BinaryWriter()) {
        for (const v of message.data) {
            MsgData.encode(v, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseTxMsgData();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.data.push(MsgData.decode(reader, reader.uint32()));
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
        return { data: globalThis.Array.isArray(object?.data) ? object.data.map((e) => MsgData.fromJSON(e)) : [] };
    },
    toJSON(message) {
        const obj = {};
        if (message.data?.length) {
            obj.data = message.data.map((e) => MsgData.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return TxMsgData.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseTxMsgData();
        message.data = object.data?.map((e) => MsgData.fromPartial(e)) || [];
        return message;
    }
};
export const SearchTxsResult = {
    $type: "cosmos.base.abci.v1beta1.SearchTxsResult",
    encode(message, writer = new BinaryWriter()) {
        if (message.total_count !== 0) {
            writer.uint32(8).uint64(message.total_count);
        }
        if (message.count !== 0) {
            writer.uint32(16).uint64(message.count);
        }
        if (message.page_number !== 0) {
            writer.uint32(24).uint64(message.page_number);
        }
        if (message.page_total !== 0) {
            writer.uint32(32).uint64(message.page_total);
        }
        if (message.limit !== 0) {
            writer.uint32(40).uint64(message.limit);
        }
        for (const v of message.txs) {
            TxResponse.encode(v, writer.uint32(50).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseSearchTxsResult();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.total_count = longToNumber(reader.uint64());
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.count = longToNumber(reader.uint64());
                    continue;
                case 3:
                    if (tag !== 24) {
                        break;
                    }
                    message.page_number = longToNumber(reader.uint64());
                    continue;
                case 4:
                    if (tag !== 32) {
                        break;
                    }
                    message.page_total = longToNumber(reader.uint64());
                    continue;
                case 5:
                    if (tag !== 40) {
                        break;
                    }
                    message.limit = longToNumber(reader.uint64());
                    continue;
                case 6:
                    if (tag !== 50) {
                        break;
                    }
                    message.txs.push(TxResponse.decode(reader, reader.uint32()));
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
            total_count: isSet(object.total_count) ? globalThis.Number(object.total_count) : 0,
            count: isSet(object.count) ? globalThis.Number(object.count) : 0,
            page_number: isSet(object.page_number) ? globalThis.Number(object.page_number) : 0,
            page_total: isSet(object.page_total) ? globalThis.Number(object.page_total) : 0,
            limit: isSet(object.limit) ? globalThis.Number(object.limit) : 0,
            txs: globalThis.Array.isArray(object?.txs) ? object.txs.map((e) => TxResponse.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.total_count !== 0) {
            obj.total_count = Math.round(message.total_count);
        }
        if (message.count !== 0) {
            obj.count = Math.round(message.count);
        }
        if (message.page_number !== 0) {
            obj.page_number = Math.round(message.page_number);
        }
        if (message.page_total !== 0) {
            obj.page_total = Math.round(message.page_total);
        }
        if (message.limit !== 0) {
            obj.limit = Math.round(message.limit);
        }
        if (message.txs?.length) {
            obj.txs = message.txs.map((e) => TxResponse.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return SearchTxsResult.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseSearchTxsResult();
        message.total_count = object.total_count ?? 0;
        message.count = object.count ?? 0;
        message.page_number = object.page_number ?? 0;
        message.page_total = object.page_total ?? 0;
        message.limit = object.limit ?? 0;
        message.txs = object.txs?.map((e) => TxResponse.fromPartial(e)) || [];
        return message;
    }
};
function createBaseTxResponse() {
    return {
        height: 0,
        txhash: "",
        codespace: "",
        code: 0,
        data: "",
        raw_log: "",
        logs: [],
        info: "",
        gas_wanted: 0,
        gas_used: 0,
        tx: undefined,
        timestamp: "",
        events: []
    };
}
function createBaseABCIMessageLog() {
    return { msg_index: 0, log: "", events: [] };
}
function createBaseStringEvent() {
    return { type: "", attributes: [] };
}
function createBaseAttribute() {
    return { key: "", value: "" };
}
function createBaseGasInfo() {
    return { gas_wanted: 0, gas_used: 0, gas_estimate: 0 };
}
function createBaseResult() {
    return { data: new Uint8Array(0), log: "", events: [], evmError: "" };
}
function createBaseSimulationResponse() {
    return { gas_info: undefined, result: undefined };
}
function createBaseMsgData() {
    return { msg_type: "", data: new Uint8Array(0) };
}
function createBaseTxMsgData() {
    return { data: [] };
}
function createBaseSearchTxsResult() {
    return { total_count: 0, count: 0, page_number: 0, page_total: 0, limit: 0, txs: [] };
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
    ["/cosmos.base.abci.v1beta1.TxResponse", TxResponse],
    ["/cosmos.base.abci.v1beta1.ABCIMessageLog", ABCIMessageLog],
    ["/cosmos.base.abci.v1beta1.StringEvent", StringEvent],
    ["/cosmos.base.abci.v1beta1.Attribute", Attribute],
    ["/cosmos.base.abci.v1beta1.GasInfo", GasInfo],
    ["/cosmos.base.abci.v1beta1.Result", Result],
    ["/cosmos.base.abci.v1beta1.SimulationResponse", SimulationResponse],
    ["/cosmos.base.abci.v1beta1.MsgData", MsgData],
    ["/cosmos.base.abci.v1beta1.TxMsgData", TxMsgData],
    ["/cosmos.base.abci.v1beta1.SearchTxsResult", SearchTxsResult]
];
export const aminoConverters = {
    "/cosmos.base.abci.v1beta1.TxResponse": {
        aminoType: "cosmos-sdk/TxResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.base.abci.v1beta1.ABCIMessageLog": {
        aminoType: "cosmos-sdk/ABCIMessageLog",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.base.abci.v1beta1.StringEvent": {
        aminoType: "cosmos-sdk/StringEvent",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.base.abci.v1beta1.Attribute": {
        aminoType: "cosmos-sdk/Attribute",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.base.abci.v1beta1.GasInfo": {
        aminoType: "cosmos-sdk/GasInfo",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.base.abci.v1beta1.Result": {
        aminoType: "cosmos-sdk/Result",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.base.abci.v1beta1.SimulationResponse": {
        aminoType: "cosmos-sdk/SimulationResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.base.abci.v1beta1.MsgData": {
        aminoType: "cosmos-sdk/MsgData",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.base.abci.v1beta1.TxMsgData": {
        aminoType: "cosmos-sdk/TxMsgData",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.base.abci.v1beta1.SearchTxsResult": {
        aminoType: "cosmos-sdk/SearchTxsResult",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
