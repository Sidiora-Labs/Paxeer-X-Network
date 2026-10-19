import { BinaryReader, BinaryWriter } from "@bufbuild/protobuf/wire";
import { pointerTypeFromJSON, pointerTypeToJSON } from "./enums";
export const QueryPaxAddressByEVMAddressRequest = {
    $type: "paxprotocol.paxchain.evm.QueryPaxAddressByEVMAddressRequest",
    encode(message, writer = new BinaryWriter()) {
        if (message.evm_address !== "") {
            writer.uint32(10).string(message.evm_address);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryPaxAddressByEVMAddressRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.evm_address = reader.string();
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
        return { evm_address: isSet(object.evm_address) ? globalThis.String(object.evm_address) : "" };
    },
    toJSON(message) {
        const obj = {};
        if (message.evm_address !== "") {
            obj.evm_address = message.evm_address;
        }
        return obj;
    },
    create(base) {
        return QueryPaxAddressByEVMAddressRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryPaxAddressByEVMAddressRequest();
        message.evm_address = object.evm_address ?? "";
        return message;
    }
};
export const QueryPaxAddressByEVMAddressResponse = {
    $type: "paxprotocol.paxchain.evm.QueryPaxAddressByEVMAddressResponse",
    encode(message, writer = new BinaryWriter()) {
        if (message.pax_address !== "") {
            writer.uint32(10).string(message.pax_address);
        }
        if (message.associated !== false) {
            writer.uint32(16).bool(message.associated);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryPaxAddressByEVMAddressResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.pax_address = reader.string();
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.associated = reader.bool();
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
            pax_address: isSet(object.pax_address) ? globalThis.String(object.pax_address) : "",
            associated: isSet(object.associated) ? globalThis.Boolean(object.associated) : false
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.pax_address !== "") {
            obj.pax_address = message.pax_address;
        }
        if (message.associated !== false) {
            obj.associated = message.associated;
        }
        return obj;
    },
    create(base) {
        return QueryPaxAddressByEVMAddressResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryPaxAddressByEVMAddressResponse();
        message.pax_address = object.pax_address ?? "";
        message.associated = object.associated ?? false;
        return message;
    }
};
export const QueryEVMAddressByPaxAddressRequest = {
    $type: "paxprotocol.paxchain.evm.QueryEVMAddressByPaxAddressRequest",
    encode(message, writer = new BinaryWriter()) {
        if (message.pax_address !== "") {
            writer.uint32(10).string(message.pax_address);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryEVMAddressByPaxAddressRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.pax_address = reader.string();
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
        return { pax_address: isSet(object.pax_address) ? globalThis.String(object.pax_address) : "" };
    },
    toJSON(message) {
        const obj = {};
        if (message.pax_address !== "") {
            obj.pax_address = message.pax_address;
        }
        return obj;
    },
    create(base) {
        return QueryEVMAddressByPaxAddressRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryEVMAddressByPaxAddressRequest();
        message.pax_address = object.pax_address ?? "";
        return message;
    }
};
export const QueryEVMAddressByPaxAddressResponse = {
    $type: "paxprotocol.paxchain.evm.QueryEVMAddressByPaxAddressResponse",
    encode(message, writer = new BinaryWriter()) {
        if (message.evm_address !== "") {
            writer.uint32(10).string(message.evm_address);
        }
        if (message.associated !== false) {
            writer.uint32(16).bool(message.associated);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryEVMAddressByPaxAddressResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.evm_address = reader.string();
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.associated = reader.bool();
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
            evm_address: isSet(object.evm_address) ? globalThis.String(object.evm_address) : "",
            associated: isSet(object.associated) ? globalThis.Boolean(object.associated) : false
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.evm_address !== "") {
            obj.evm_address = message.evm_address;
        }
        if (message.associated !== false) {
            obj.associated = message.associated;
        }
        return obj;
    },
    create(base) {
        return QueryEVMAddressByPaxAddressResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryEVMAddressByPaxAddressResponse();
        message.evm_address = object.evm_address ?? "";
        message.associated = object.associated ?? false;
        return message;
    }
};
export const QueryStaticCallRequest = {
    $type: "paxprotocol.paxchain.evm.QueryStaticCallRequest",
    encode(message, writer = new BinaryWriter()) {
        if (message.data.length !== 0) {
            writer.uint32(10).bytes(message.data);
        }
        if (message.to !== "") {
            writer.uint32(18).string(message.to);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryStaticCallRequest();
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
                    message.to = reader.string();
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
            to: isSet(object.to) ? globalThis.String(object.to) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.data.length !== 0) {
            obj.data = base64FromBytes(message.data);
        }
        if (message.to !== "") {
            obj.to = message.to;
        }
        return obj;
    },
    create(base) {
        return QueryStaticCallRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryStaticCallRequest();
        message.data = object.data ?? new Uint8Array(0);
        message.to = object.to ?? "";
        return message;
    }
};
export const QueryStaticCallResponse = {
    $type: "paxprotocol.paxchain.evm.QueryStaticCallResponse",
    encode(message, writer = new BinaryWriter()) {
        if (message.data.length !== 0) {
            writer.uint32(10).bytes(message.data);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryStaticCallResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
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
        return { data: isSet(object.data) ? bytesFromBase64(object.data) : new Uint8Array(0) };
    },
    toJSON(message) {
        const obj = {};
        if (message.data.length !== 0) {
            obj.data = base64FromBytes(message.data);
        }
        return obj;
    },
    create(base) {
        return QueryStaticCallResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryStaticCallResponse();
        message.data = object.data ?? new Uint8Array(0);
        return message;
    }
};
export const QueryPointerRequest = {
    $type: "paxprotocol.paxchain.evm.QueryPointerRequest",
    encode(message, writer = new BinaryWriter()) {
        if (message.pointer_type !== 0) {
            writer.uint32(8).int32(message.pointer_type);
        }
        if (message.pointee !== "") {
            writer.uint32(18).string(message.pointee);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryPointerRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.pointer_type = reader.int32();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.pointee = reader.string();
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
            pointer_type: isSet(object.pointer_type) ? pointerTypeFromJSON(object.pointer_type) : 0,
            pointee: isSet(object.pointee) ? globalThis.String(object.pointee) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.pointer_type !== 0) {
            obj.pointer_type = pointerTypeToJSON(message.pointer_type);
        }
        if (message.pointee !== "") {
            obj.pointee = message.pointee;
        }
        return obj;
    },
    create(base) {
        return QueryPointerRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryPointerRequest();
        message.pointer_type = object.pointer_type ?? 0;
        message.pointee = object.pointee ?? "";
        return message;
    }
};
export const QueryPointerResponse = {
    $type: "paxprotocol.paxchain.evm.QueryPointerResponse",
    encode(message, writer = new BinaryWriter()) {
        if (message.pointer !== "") {
            writer.uint32(10).string(message.pointer);
        }
        if (message.version !== 0) {
            writer.uint32(16).uint32(message.version);
        }
        if (message.exists !== false) {
            writer.uint32(24).bool(message.exists);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryPointerResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.pointer = reader.string();
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.version = reader.uint32();
                    continue;
                case 3:
                    if (tag !== 24) {
                        break;
                    }
                    message.exists = reader.bool();
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
            pointer: isSet(object.pointer) ? globalThis.String(object.pointer) : "",
            version: isSet(object.version) ? globalThis.Number(object.version) : 0,
            exists: isSet(object.exists) ? globalThis.Boolean(object.exists) : false
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.pointer !== "") {
            obj.pointer = message.pointer;
        }
        if (message.version !== 0) {
            obj.version = Math.round(message.version);
        }
        if (message.exists !== false) {
            obj.exists = message.exists;
        }
        return obj;
    },
    create(base) {
        return QueryPointerResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryPointerResponse();
        message.pointer = object.pointer ?? "";
        message.version = object.version ?? 0;
        message.exists = object.exists ?? false;
        return message;
    }
};
export const QueryPointerVersionRequest = {
    $type: "paxprotocol.paxchain.evm.QueryPointerVersionRequest",
    encode(message, writer = new BinaryWriter()) {
        if (message.pointer_type !== 0) {
            writer.uint32(8).int32(message.pointer_type);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryPointerVersionRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.pointer_type = reader.int32();
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
        return { pointer_type: isSet(object.pointer_type) ? pointerTypeFromJSON(object.pointer_type) : 0 };
    },
    toJSON(message) {
        const obj = {};
        if (message.pointer_type !== 0) {
            obj.pointer_type = pointerTypeToJSON(message.pointer_type);
        }
        return obj;
    },
    create(base) {
        return QueryPointerVersionRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryPointerVersionRequest();
        message.pointer_type = object.pointer_type ?? 0;
        return message;
    }
};
export const QueryPointerVersionResponse = {
    $type: "paxprotocol.paxchain.evm.QueryPointerVersionResponse",
    encode(message, writer = new BinaryWriter()) {
        if (message.version !== 0) {
            writer.uint32(8).uint32(message.version);
        }
        if (message.cw_code_id !== 0) {
            writer.uint32(16).uint64(message.cw_code_id);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryPointerVersionResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.version = reader.uint32();
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.cw_code_id = longToNumber(reader.uint64());
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
            version: isSet(object.version) ? globalThis.Number(object.version) : 0,
            cw_code_id: isSet(object.cw_code_id) ? globalThis.Number(object.cw_code_id) : 0
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.version !== 0) {
            obj.version = Math.round(message.version);
        }
        if (message.cw_code_id !== 0) {
            obj.cw_code_id = Math.round(message.cw_code_id);
        }
        return obj;
    },
    create(base) {
        return QueryPointerVersionResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryPointerVersionResponse();
        message.version = object.version ?? 0;
        message.cw_code_id = object.cw_code_id ?? 0;
        return message;
    }
};
export const QueryPointeeRequest = {
    $type: "paxprotocol.paxchain.evm.QueryPointeeRequest",
    encode(message, writer = new BinaryWriter()) {
        if (message.pointer_type !== 0) {
            writer.uint32(8).int32(message.pointer_type);
        }
        if (message.pointer !== "") {
            writer.uint32(18).string(message.pointer);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryPointeeRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.pointer_type = reader.int32();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.pointer = reader.string();
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
            pointer_type: isSet(object.pointer_type) ? pointerTypeFromJSON(object.pointer_type) : 0,
            pointer: isSet(object.pointer) ? globalThis.String(object.pointer) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.pointer_type !== 0) {
            obj.pointer_type = pointerTypeToJSON(message.pointer_type);
        }
        if (message.pointer !== "") {
            obj.pointer = message.pointer;
        }
        return obj;
    },
    create(base) {
        return QueryPointeeRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryPointeeRequest();
        message.pointer_type = object.pointer_type ?? 0;
        message.pointer = object.pointer ?? "";
        return message;
    }
};
export const QueryPointeeResponse = {
    $type: "paxprotocol.paxchain.evm.QueryPointeeResponse",
    encode(message, writer = new BinaryWriter()) {
        if (message.pointee !== "") {
            writer.uint32(10).string(message.pointee);
        }
        if (message.version !== 0) {
            writer.uint32(16).uint32(message.version);
        }
        if (message.exists !== false) {
            writer.uint32(24).bool(message.exists);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryPointeeResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.pointee = reader.string();
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.version = reader.uint32();
                    continue;
                case 3:
                    if (tag !== 24) {
                        break;
                    }
                    message.exists = reader.bool();
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
            pointee: isSet(object.pointee) ? globalThis.String(object.pointee) : "",
            version: isSet(object.version) ? globalThis.Number(object.version) : 0,
            exists: isSet(object.exists) ? globalThis.Boolean(object.exists) : false
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.pointee !== "") {
            obj.pointee = message.pointee;
        }
        if (message.version !== 0) {
            obj.version = Math.round(message.version);
        }
        if (message.exists !== false) {
            obj.exists = message.exists;
        }
        return obj;
    },
    create(base) {
        return QueryPointeeResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryPointeeResponse();
        message.pointee = object.pointee ?? "";
        message.version = object.version ?? 0;
        message.exists = object.exists ?? false;
        return message;
    }
};
function createBaseQueryPaxAddressByEVMAddressRequest() {
    return { evm_address: "" };
}
function createBaseQueryPaxAddressByEVMAddressResponse() {
    return { pax_address: "", associated: false };
}
function createBaseQueryEVMAddressByPaxAddressRequest() {
    return { pax_address: "" };
}
function createBaseQueryEVMAddressByPaxAddressResponse() {
    return { evm_address: "", associated: false };
}
function createBaseQueryStaticCallRequest() {
    return { data: new Uint8Array(0), to: "" };
}
function createBaseQueryStaticCallResponse() {
    return { data: new Uint8Array(0) };
}
function createBaseQueryPointerRequest() {
    return { pointer_type: 0, pointee: "" };
}
function createBaseQueryPointerResponse() {
    return { pointer: "", version: 0, exists: false };
}
function createBaseQueryPointerVersionRequest() {
    return { pointer_type: 0 };
}
function createBaseQueryPointerVersionResponse() {
    return { version: 0, cw_code_id: 0 };
}
function createBaseQueryPointeeRequest() {
    return { pointer_type: 0, pointer: "" };
}
function createBaseQueryPointeeResponse() {
    return { pointee: "", version: 0, exists: false };
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
    ["/paxprotocol.paxchain.evm.QueryPaxAddressByEVMAddressRequest", QueryPaxAddressByEVMAddressRequest],
    ["/paxprotocol.paxchain.evm.QueryPaxAddressByEVMAddressResponse", QueryPaxAddressByEVMAddressResponse],
    ["/paxprotocol.paxchain.evm.QueryEVMAddressByPaxAddressRequest", QueryEVMAddressByPaxAddressRequest],
    ["/paxprotocol.paxchain.evm.QueryEVMAddressByPaxAddressResponse", QueryEVMAddressByPaxAddressResponse],
    ["/paxprotocol.paxchain.evm.QueryStaticCallRequest", QueryStaticCallRequest],
    ["/paxprotocol.paxchain.evm.QueryStaticCallResponse", QueryStaticCallResponse],
    ["/paxprotocol.paxchain.evm.QueryPointerRequest", QueryPointerRequest],
    ["/paxprotocol.paxchain.evm.QueryPointerResponse", QueryPointerResponse],
    ["/paxprotocol.paxchain.evm.QueryPointerVersionRequest", QueryPointerVersionRequest],
    ["/paxprotocol.paxchain.evm.QueryPointerVersionResponse", QueryPointerVersionResponse],
    ["/paxprotocol.paxchain.evm.QueryPointeeRequest", QueryPointeeRequest],
    ["/paxprotocol.paxchain.evm.QueryPointeeResponse", QueryPointeeResponse]
];
export const aminoConverters = {
    "/paxprotocol.paxchain.evm.QueryPaxAddressByEVMAddressRequest": {
        aminoType: "evm/QueryPaxAddressByEVMAddressRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.evm.QueryPaxAddressByEVMAddressResponse": {
        aminoType: "evm/QueryPaxAddressByEVMAddressResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.evm.QueryEVMAddressByPaxAddressRequest": {
        aminoType: "evm/QueryEVMAddressByPaxAddressRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.evm.QueryEVMAddressByPaxAddressResponse": {
        aminoType: "evm/QueryEVMAddressByPaxAddressResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.evm.QueryStaticCallRequest": {
        aminoType: "evm/QueryStaticCallRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.evm.QueryStaticCallResponse": {
        aminoType: "evm/QueryStaticCallResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.evm.QueryPointerRequest": {
        aminoType: "evm/QueryPointerRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.evm.QueryPointerResponse": {
        aminoType: "evm/QueryPointerResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.evm.QueryPointerVersionRequest": {
        aminoType: "evm/QueryPointerVersionRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.evm.QueryPointerVersionResponse": {
        aminoType: "evm/QueryPointerVersionResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.evm.QueryPointeeRequest": {
        aminoType: "evm/QueryPointeeRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.evm.QueryPointeeResponse": {
        aminoType: "evm/QueryPointeeResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
