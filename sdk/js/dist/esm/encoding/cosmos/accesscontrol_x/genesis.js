import { BinaryReader, BinaryWriter } from "@bufbuild/protobuf/wire";
import { MessageDependencyMapping, WasmDependencyMapping } from "../accesscontrol/accesscontrol";
export const GenesisState = {
    $type: "cosmos.accesscontrol_x.v1beta1.GenesisState",
    encode(message, writer = new BinaryWriter()) {
        if (message.params !== undefined) {
            Params.encode(message.params, writer.uint32(10).fork()).join();
        }
        for (const v of message.message_dependency_mapping) {
            MessageDependencyMapping.encode(v, writer.uint32(18).fork()).join();
        }
        for (const v of message.wasm_dependency_mappings) {
            WasmDependencyMapping.encode(v, writer.uint32(26).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseGenesisState();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.params = Params.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.message_dependency_mapping.push(MessageDependencyMapping.decode(reader, reader.uint32()));
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.wasm_dependency_mappings.push(WasmDependencyMapping.decode(reader, reader.uint32()));
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
            params: isSet(object.params) ? Params.fromJSON(object.params) : undefined,
            message_dependency_mapping: globalThis.Array.isArray(object?.message_dependency_mapping)
                ? object.message_dependency_mapping.map((e) => MessageDependencyMapping.fromJSON(e))
                : [],
            wasm_dependency_mappings: globalThis.Array.isArray(object?.wasm_dependency_mappings)
                ? object.wasm_dependency_mappings.map((e) => WasmDependencyMapping.fromJSON(e))
                : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.params !== undefined) {
            obj.params = Params.toJSON(message.params);
        }
        if (message.message_dependency_mapping?.length) {
            obj.message_dependency_mapping = message.message_dependency_mapping.map((e) => MessageDependencyMapping.toJSON(e));
        }
        if (message.wasm_dependency_mappings?.length) {
            obj.wasm_dependency_mappings = message.wasm_dependency_mappings.map((e) => WasmDependencyMapping.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return GenesisState.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGenesisState();
        message.params = object.params !== undefined && object.params !== null ? Params.fromPartial(object.params) : undefined;
        message.message_dependency_mapping = object.message_dependency_mapping?.map((e) => MessageDependencyMapping.fromPartial(e)) || [];
        message.wasm_dependency_mappings = object.wasm_dependency_mappings?.map((e) => WasmDependencyMapping.fromPartial(e)) || [];
        return message;
    }
};
export const Params = {
    $type: "cosmos.accesscontrol_x.v1beta1.Params",
    encode(_, writer = new BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseParams();
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
        return Params.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseParams();
        return message;
    }
};
function createBaseGenesisState() {
    return { params: undefined, message_dependency_mapping: [], wasm_dependency_mappings: [] };
}
function createBaseParams() {
    return {};
}
function isSet(value) {
    return value !== null && value !== undefined;
}
export const registry = [
    ["/cosmos.accesscontrol_x.v1beta1.GenesisState", GenesisState],
    ["/cosmos.accesscontrol_x.v1beta1.Params", Params]
];
export const aminoConverters = {
    "/cosmos.accesscontrol_x.v1beta1.GenesisState": {
        aminoType: "cosmos-sdk/GenesisState",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.accesscontrol_x.v1beta1.Params": {
        aminoType: "cosmos-sdk/Params",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
