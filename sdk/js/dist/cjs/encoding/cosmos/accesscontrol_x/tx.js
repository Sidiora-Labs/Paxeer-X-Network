"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.MsgRegisterWasmDependencyResponse = exports.MsgRegisterWasmDependency = exports.RegisterWasmDependencyJSONFile = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
const accesscontrol_1 = require("../accesscontrol/accesscontrol");
exports.RegisterWasmDependencyJSONFile = {
    $type: "cosmos.accesscontrol_x.v1beta1.RegisterWasmDependencyJSONFile",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.wasm_dependency_mapping !== undefined) {
            accesscontrol_1.WasmDependencyMapping.encode(message.wasm_dependency_mapping, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseRegisterWasmDependencyJSONFile();
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
        return exports.RegisterWasmDependencyJSONFile.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseRegisterWasmDependencyJSONFile();
        message.wasm_dependency_mapping =
            object.wasm_dependency_mapping !== undefined && object.wasm_dependency_mapping !== null
                ? accesscontrol_1.WasmDependencyMapping.fromPartial(object.wasm_dependency_mapping)
                : undefined;
        return message;
    }
};
exports.MsgRegisterWasmDependency = {
    $type: "cosmos.accesscontrol_x.v1beta1.MsgRegisterWasmDependency",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.from_address !== "") {
            writer.uint32(10).string(message.from_address);
        }
        if (message.wasm_dependency_mapping !== undefined) {
            accesscontrol_1.WasmDependencyMapping.encode(message.wasm_dependency_mapping, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgRegisterWasmDependency();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.from_address = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
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
            from_address: isSet(object.from_address) ? globalThis.String(object.from_address) : "",
            wasm_dependency_mapping: isSet(object.wasm_dependency_mapping) ? accesscontrol_1.WasmDependencyMapping.fromJSON(object.wasm_dependency_mapping) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.from_address !== "") {
            obj.from_address = message.from_address;
        }
        if (message.wasm_dependency_mapping !== undefined) {
            obj.wasm_dependency_mapping = accesscontrol_1.WasmDependencyMapping.toJSON(message.wasm_dependency_mapping);
        }
        return obj;
    },
    create(base) {
        return exports.MsgRegisterWasmDependency.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgRegisterWasmDependency();
        message.from_address = object.from_address ?? "";
        message.wasm_dependency_mapping =
            object.wasm_dependency_mapping !== undefined && object.wasm_dependency_mapping !== null
                ? accesscontrol_1.WasmDependencyMapping.fromPartial(object.wasm_dependency_mapping)
                : undefined;
        return message;
    }
};
exports.MsgRegisterWasmDependencyResponse = {
    $type: "cosmos.accesscontrol_x.v1beta1.MsgRegisterWasmDependencyResponse",
    encode(_, writer = new wire_1.BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgRegisterWasmDependencyResponse();
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
        return exports.MsgRegisterWasmDependencyResponse.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseMsgRegisterWasmDependencyResponse();
        return message;
    }
};
function createBaseRegisterWasmDependencyJSONFile() {
    return { wasm_dependency_mapping: undefined };
}
function createBaseMsgRegisterWasmDependency() {
    return { from_address: "", wasm_dependency_mapping: undefined };
}
function createBaseMsgRegisterWasmDependencyResponse() {
    return {};
}
function isSet(value) {
    return value !== null && value !== undefined;
}
exports.registry = [
    ["/cosmos.accesscontrol_x.v1beta1.RegisterWasmDependencyJSONFile", exports.RegisterWasmDependencyJSONFile],
    ["/cosmos.accesscontrol_x.v1beta1.MsgRegisterWasmDependency", exports.MsgRegisterWasmDependency],
    ["/cosmos.accesscontrol_x.v1beta1.MsgRegisterWasmDependencyResponse", exports.MsgRegisterWasmDependencyResponse]
];
exports.aminoConverters = {
    "/cosmos.accesscontrol_x.v1beta1.RegisterWasmDependencyJSONFile": {
        aminoType: "cosmos-sdk/RegisterWasmDependencyJSONFile",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.accesscontrol_x.v1beta1.MsgRegisterWasmDependency": {
        aminoType: "cosmos-sdk/MsgRegisterWasmDependency",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.accesscontrol_x.v1beta1.MsgRegisterWasmDependencyResponse": {
        aminoType: "cosmos-sdk/MsgRegisterWasmDependencyResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
