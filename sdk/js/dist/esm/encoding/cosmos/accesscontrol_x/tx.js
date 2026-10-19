import { BinaryReader, BinaryWriter } from "@bufbuild/protobuf/wire";
import { WasmDependencyMapping } from "../accesscontrol/accesscontrol";
export const RegisterWasmDependencyJSONFile = {
    $type: "cosmos.accesscontrol_x.v1beta1.RegisterWasmDependencyJSONFile",
    encode(message, writer = new BinaryWriter()) {
        if (message.wasm_dependency_mapping !== undefined) {
            WasmDependencyMapping.encode(message.wasm_dependency_mapping, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseRegisterWasmDependencyJSONFile();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.wasm_dependency_mapping = WasmDependencyMapping.decode(reader, reader.uint32());
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
            wasm_dependency_mapping: isSet(object.wasm_dependency_mapping) ? WasmDependencyMapping.fromJSON(object.wasm_dependency_mapping) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.wasm_dependency_mapping !== undefined) {
            obj.wasm_dependency_mapping = WasmDependencyMapping.toJSON(message.wasm_dependency_mapping);
        }
        return obj;
    },
    create(base) {
        return RegisterWasmDependencyJSONFile.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseRegisterWasmDependencyJSONFile();
        message.wasm_dependency_mapping =
            object.wasm_dependency_mapping !== undefined && object.wasm_dependency_mapping !== null
                ? WasmDependencyMapping.fromPartial(object.wasm_dependency_mapping)
                : undefined;
        return message;
    }
};
export const MsgRegisterWasmDependency = {
    $type: "cosmos.accesscontrol_x.v1beta1.MsgRegisterWasmDependency",
    encode(message, writer = new BinaryWriter()) {
        if (message.from_address !== "") {
            writer.uint32(10).string(message.from_address);
        }
        if (message.wasm_dependency_mapping !== undefined) {
            WasmDependencyMapping.encode(message.wasm_dependency_mapping, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
                    message.wasm_dependency_mapping = WasmDependencyMapping.decode(reader, reader.uint32());
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
            wasm_dependency_mapping: isSet(object.wasm_dependency_mapping) ? WasmDependencyMapping.fromJSON(object.wasm_dependency_mapping) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.from_address !== "") {
            obj.from_address = message.from_address;
        }
        if (message.wasm_dependency_mapping !== undefined) {
            obj.wasm_dependency_mapping = WasmDependencyMapping.toJSON(message.wasm_dependency_mapping);
        }
        return obj;
    },
    create(base) {
        return MsgRegisterWasmDependency.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgRegisterWasmDependency();
        message.from_address = object.from_address ?? "";
        message.wasm_dependency_mapping =
            object.wasm_dependency_mapping !== undefined && object.wasm_dependency_mapping !== null
                ? WasmDependencyMapping.fromPartial(object.wasm_dependency_mapping)
                : undefined;
        return message;
    }
};
export const MsgRegisterWasmDependencyResponse = {
    $type: "cosmos.accesscontrol_x.v1beta1.MsgRegisterWasmDependencyResponse",
    encode(_, writer = new BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
        return MsgRegisterWasmDependencyResponse.fromPartial(base ?? {});
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
export const registry = [
    ["/cosmos.accesscontrol_x.v1beta1.RegisterWasmDependencyJSONFile", RegisterWasmDependencyJSONFile],
    ["/cosmos.accesscontrol_x.v1beta1.MsgRegisterWasmDependency", MsgRegisterWasmDependency],
    ["/cosmos.accesscontrol_x.v1beta1.MsgRegisterWasmDependencyResponse", MsgRegisterWasmDependencyResponse]
];
export const aminoConverters = {
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
