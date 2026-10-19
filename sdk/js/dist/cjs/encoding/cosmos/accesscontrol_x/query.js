"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.ListWasmDependencyMappingResponse = exports.ListWasmDependencyMappingRequest = exports.ListResourceDependencyMappingResponse = exports.ListResourceDependencyMappingRequest = exports.WasmDependencyMappingResponse = exports.WasmDependencyMappingRequest = exports.ResourceDependencyMappingFromMessageKeyResponse = exports.ResourceDependencyMappingFromMessageKeyRequest = exports.QueryParamsResponse = exports.QueryParamsRequest = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
const accesscontrol_1 = require("../accesscontrol/accesscontrol");
const genesis_1 = require("./genesis");
exports.QueryParamsRequest = {
    $type: "cosmos.accesscontrol_x.v1beta1.QueryParamsRequest",
    encode(_, writer = new wire_1.BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryParamsRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
            }
            if ((tag & 7) === 4 || tag === 0) {
                break;
            }
            reader.skip(tag & 7);
        }
        return message;
    },
    fromJSON(_) {
        return {};
    },
    toJSON(_) {
        const obj = {};
        return obj;
    },
    create(base) {
        return exports.QueryParamsRequest.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseQueryParamsRequest();
        return message;
    }
};
exports.QueryParamsResponse = {
    $type: "cosmos.accesscontrol_x.v1beta1.QueryParamsResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.params !== undefined) {
            genesis_1.Params.encode(message.params, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryParamsResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.params = genesis_1.Params.decode(reader, reader.uint32());
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
        return { params: isSet(object.params) ? genesis_1.Params.fromJSON(object.params) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.params !== undefined) {
            obj.params = genesis_1.Params.toJSON(message.params);
        }
        return obj;
    },
    create(base) {
        return exports.QueryParamsResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryParamsResponse();
        message.params = object.params !== undefined && object.params !== null ? genesis_1.Params.fromPartial(object.params) : undefined;
        return message;
    }
};
exports.ResourceDependencyMappingFromMessageKeyRequest = {
    $type: "cosmos.accesscontrol_x.v1beta1.ResourceDependencyMappingFromMessageKeyRequest",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.message_key !== "") {
            writer.uint32(10).string(message.message_key);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseResourceDependencyMappingFromMessageKeyRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.message_key = reader.string();
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
        return { message_key: isSet(object.message_key) ? globalThis.String(object.message_key) : "" };
    },
    toJSON(message) {
        const obj = {};
        if (message.message_key !== "") {
            obj.message_key = message.message_key;
        }
        return obj;
    },
    create(base) {
        return exports.ResourceDependencyMappingFromMessageKeyRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseResourceDependencyMappingFromMessageKeyRequest();
        message.message_key = object.message_key ?? "";
        return message;
    }
};
exports.ResourceDependencyMappingFromMessageKeyResponse = {
    $type: "cosmos.accesscontrol_x.v1beta1.ResourceDependencyMappingFromMessageKeyResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.message_dependency_mapping !== undefined) {
            accesscontrol_1.MessageDependencyMapping.encode(message.message_dependency_mapping, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseResourceDependencyMappingFromMessageKeyResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.message_dependency_mapping = accesscontrol_1.MessageDependencyMapping.decode(reader, reader.uint32());
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
            message_dependency_mapping: isSet(object.message_dependency_mapping) ? accesscontrol_1.MessageDependencyMapping.fromJSON(object.message_dependency_mapping) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.message_dependency_mapping !== undefined) {
            obj.message_dependency_mapping = accesscontrol_1.MessageDependencyMapping.toJSON(message.message_dependency_mapping);
        }
        return obj;
    },
    create(base) {
        return exports.ResourceDependencyMappingFromMessageKeyResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseResourceDependencyMappingFromMessageKeyResponse();
        message.message_dependency_mapping =
            object.message_dependency_mapping !== undefined && object.message_dependency_mapping !== null
                ? accesscontrol_1.MessageDependencyMapping.fromPartial(object.message_dependency_mapping)
                : undefined;
        return message;
    }
};
exports.WasmDependencyMappingRequest = {
    $type: "cosmos.accesscontrol_x.v1beta1.WasmDependencyMappingRequest",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.contract_address !== "") {
            writer.uint32(10).string(message.contract_address);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseWasmDependencyMappingRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.contract_address = reader.string();
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
        return { contract_address: isSet(object.contract_address) ? globalThis.String(object.contract_address) : "" };
    },
    toJSON(message) {
        const obj = {};
        if (message.contract_address !== "") {
            obj.contract_address = message.contract_address;
        }
        return obj;
    },
    create(base) {
        return exports.WasmDependencyMappingRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseWasmDependencyMappingRequest();
        message.contract_address = object.contract_address ?? "";
        return message;
    }
};
exports.WasmDependencyMappingResponse = {
    $type: "cosmos.accesscontrol_x.v1beta1.WasmDependencyMappingResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.wasm_dependency_mapping !== undefined) {
            accesscontrol_1.WasmDependencyMapping.encode(message.wasm_dependency_mapping, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseWasmDependencyMappingResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.wasm_dependency_mapping = accesscontrol_1.WasmDependencyMapping.decode(reader, reader.uint32());
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
            wasm_dependency_mapping: isSet(object.wasm_dependency_mapping) ? accesscontrol_1.WasmDependencyMapping.fromJSON(object.wasm_dependency_mapping) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.wasm_dependency_mapping !== undefined) {
            obj.wasm_dependency_mapping = accesscontrol_1.WasmDependencyMapping.toJSON(message.wasm_dependency_mapping);
        }
        return obj;
    },
    create(base) {
        return exports.WasmDependencyMappingResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseWasmDependencyMappingResponse();
        message.wasm_dependency_mapping =
            object.wasm_dependency_mapping !== undefined && object.wasm_dependency_mapping !== null
                ? accesscontrol_1.WasmDependencyMapping.fromPartial(object.wasm_dependency_mapping)
                : undefined;
        return message;
    }
};
exports.ListResourceDependencyMappingRequest = {
    $type: "cosmos.accesscontrol_x.v1beta1.ListResourceDependencyMappingRequest",
    encode(_, writer = new wire_1.BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseListResourceDependencyMappingRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
            }
            if ((tag & 7) === 4 || tag === 0) {
                break;
            }
            reader.skip(tag & 7);
        }
        return message;
    },
    fromJSON(_) {
        return {};
    },
    toJSON(_) {
        const obj = {};
        return obj;
    },
    create(base) {
        return exports.ListResourceDependencyMappingRequest.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseListResourceDependencyMappingRequest();
        return message;
    }
};
exports.ListResourceDependencyMappingResponse = {
    $type: "cosmos.accesscontrol_x.v1beta1.ListResourceDependencyMappingResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.message_dependency_mapping_list) {
            accesscontrol_1.MessageDependencyMapping.encode(v, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseListResourceDependencyMappingResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.message_dependency_mapping_list.push(accesscontrol_1.MessageDependencyMapping.decode(reader, reader.uint32()));
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
            message_dependency_mapping_list: globalThis.Array.isArray(object?.message_dependency_mapping_list)
                ? object.message_dependency_mapping_list.map((e) => accesscontrol_1.MessageDependencyMapping.fromJSON(e))
                : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.message_dependency_mapping_list?.length) {
            obj.message_dependency_mapping_list = message.message_dependency_mapping_list.map((e) => accesscontrol_1.MessageDependencyMapping.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.ListResourceDependencyMappingResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseListResourceDependencyMappingResponse();
        message.message_dependency_mapping_list = object.message_dependency_mapping_list?.map((e) => accesscontrol_1.MessageDependencyMapping.fromPartial(e)) || [];
        return message;
    }
};
exports.ListWasmDependencyMappingRequest = {
    $type: "cosmos.accesscontrol_x.v1beta1.ListWasmDependencyMappingRequest",
    encode(_, writer = new wire_1.BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseListWasmDependencyMappingRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
            }
            if ((tag & 7) === 4 || tag === 0) {
                break;
            }
            reader.skip(tag & 7);
        }
        return message;
    },
    fromJSON(_) {
        return {};
    },
    toJSON(_) {
        const obj = {};
        return obj;
    },
    create(base) {
        return exports.ListWasmDependencyMappingRequest.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseListWasmDependencyMappingRequest();
        return message;
    }
};
exports.ListWasmDependencyMappingResponse = {
    $type: "cosmos.accesscontrol_x.v1beta1.ListWasmDependencyMappingResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.wasm_dependency_mapping_list) {
            accesscontrol_1.WasmDependencyMapping.encode(v, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseListWasmDependencyMappingResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.wasm_dependency_mapping_list.push(accesscontrol_1.WasmDependencyMapping.decode(reader, reader.uint32()));
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
            wasm_dependency_mapping_list: globalThis.Array.isArray(object?.wasm_dependency_mapping_list)
                ? object.wasm_dependency_mapping_list.map((e) => accesscontrol_1.WasmDependencyMapping.fromJSON(e))
                : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.wasm_dependency_mapping_list?.length) {
            obj.wasm_dependency_mapping_list = message.wasm_dependency_mapping_list.map((e) => accesscontrol_1.WasmDependencyMapping.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.ListWasmDependencyMappingResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseListWasmDependencyMappingResponse();
        message.wasm_dependency_mapping_list = object.wasm_dependency_mapping_list?.map((e) => accesscontrol_1.WasmDependencyMapping.fromPartial(e)) || [];
        return message;
    }
};
function createBaseQueryParamsRequest() {
    return {};
}
function createBaseQueryParamsResponse() {
    return { params: undefined };
}
function createBaseResourceDependencyMappingFromMessageKeyRequest() {
    return { message_key: "" };
}
function createBaseResourceDependencyMappingFromMessageKeyResponse() {
    return { message_dependency_mapping: undefined };
}
function createBaseWasmDependencyMappingRequest() {
    return { contract_address: "" };
}
function createBaseWasmDependencyMappingResponse() {
    return { wasm_dependency_mapping: undefined };
}
function createBaseListResourceDependencyMappingRequest() {
    return {};
}
function createBaseListResourceDependencyMappingResponse() {
    return { message_dependency_mapping_list: [] };
}
function createBaseListWasmDependencyMappingRequest() {
    return {};
}
function createBaseListWasmDependencyMappingResponse() {
    return { wasm_dependency_mapping_list: [] };
}
function isSet(value) {
    return value !== null && value !== undefined;
}
exports.registry = [
    ["/cosmos.accesscontrol_x.v1beta1.QueryParamsRequest", exports.QueryParamsRequest],
    ["/cosmos.accesscontrol_x.v1beta1.QueryParamsResponse", exports.QueryParamsResponse],
    ["/cosmos.accesscontrol_x.v1beta1.ResourceDependencyMappingFromMessageKeyRequest", exports.ResourceDependencyMappingFromMessageKeyRequest],
    ["/cosmos.accesscontrol_x.v1beta1.ResourceDependencyMappingFromMessageKeyResponse", exports.ResourceDependencyMappingFromMessageKeyResponse],
    ["/cosmos.accesscontrol_x.v1beta1.WasmDependencyMappingRequest", exports.WasmDependencyMappingRequest],
    ["/cosmos.accesscontrol_x.v1beta1.WasmDependencyMappingResponse", exports.WasmDependencyMappingResponse],
    ["/cosmos.accesscontrol_x.v1beta1.ListResourceDependencyMappingRequest", exports.ListResourceDependencyMappingRequest],
    ["/cosmos.accesscontrol_x.v1beta1.ListResourceDependencyMappingResponse", exports.ListResourceDependencyMappingResponse],
    ["/cosmos.accesscontrol_x.v1beta1.ListWasmDependencyMappingRequest", exports.ListWasmDependencyMappingRequest],
    ["/cosmos.accesscontrol_x.v1beta1.ListWasmDependencyMappingResponse", exports.ListWasmDependencyMappingResponse]
];
exports.aminoConverters = {
    "/cosmos.accesscontrol_x.v1beta1.QueryParamsRequest": {
        aminoType: "cosmos-sdk/QueryParamsRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.accesscontrol_x.v1beta1.QueryParamsResponse": {
        aminoType: "cosmos-sdk/QueryParamsResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.accesscontrol_x.v1beta1.ResourceDependencyMappingFromMessageKeyRequest": {
        aminoType: "cosmos-sdk/ResourceDependencyMappingFromMessageKeyRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.accesscontrol_x.v1beta1.ResourceDependencyMappingFromMessageKeyResponse": {
        aminoType: "cosmos-sdk/ResourceDependencyMappingFromMessageKeyResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.accesscontrol_x.v1beta1.WasmDependencyMappingRequest": {
        aminoType: "cosmos-sdk/WasmDependencyMappingRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.accesscontrol_x.v1beta1.WasmDependencyMappingResponse": {
        aminoType: "cosmos-sdk/WasmDependencyMappingResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.accesscontrol_x.v1beta1.ListResourceDependencyMappingRequest": {
        aminoType: "cosmos-sdk/ListResourceDependencyMappingRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.accesscontrol_x.v1beta1.ListResourceDependencyMappingResponse": {
        aminoType: "cosmos-sdk/ListResourceDependencyMappingResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.accesscontrol_x.v1beta1.ListWasmDependencyMappingRequest": {
        aminoType: "cosmos-sdk/ListWasmDependencyMappingRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.accesscontrol_x.v1beta1.ListWasmDependencyMappingResponse": {
        aminoType: "cosmos-sdk/ListWasmDependencyMappingResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
