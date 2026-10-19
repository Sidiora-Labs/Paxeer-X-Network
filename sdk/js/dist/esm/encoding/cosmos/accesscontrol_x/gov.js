import { BinaryReader, BinaryWriter } from "@bufbuild/protobuf/wire";
import { MessageDependencyMapping, WasmDependencyMapping } from "../accesscontrol/accesscontrol";
export const MsgUpdateResourceDependencyMappingProposal = {
    $type: "cosmos.accesscontrol.v1beta1.MsgUpdateResourceDependencyMappingProposal",
    encode(message, writer = new BinaryWriter()) {
        if (message.title !== "") {
            writer.uint32(10).string(message.title);
        }
        if (message.description !== "") {
            writer.uint32(18).string(message.description);
        }
        for (const v of message.message_dependency_mapping) {
            MessageDependencyMapping.encode(v, writer.uint32(26).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgUpdateResourceDependencyMappingProposal();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.title = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.description = reader.string();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.message_dependency_mapping.push(MessageDependencyMapping.decode(reader, reader.uint32()));
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
            title: isSet(object.title) ? globalThis.String(object.title) : "",
            description: isSet(object.description) ? globalThis.String(object.description) : "",
            message_dependency_mapping: globalThis.Array.isArray(object?.message_dependency_mapping)
                ? object.message_dependency_mapping.map((e) => MessageDependencyMapping.fromJSON(e))
                : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.title !== "") {
            obj.title = message.title;
        }
        if (message.description !== "") {
            obj.description = message.description;
        }
        if (message.message_dependency_mapping?.length) {
            obj.message_dependency_mapping = message.message_dependency_mapping.map((e) => MessageDependencyMapping.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return MsgUpdateResourceDependencyMappingProposal.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgUpdateResourceDependencyMappingProposal();
        message.title = object.title ?? "";
        message.description = object.description ?? "";
        message.message_dependency_mapping = object.message_dependency_mapping?.map((e) => MessageDependencyMapping.fromPartial(e)) || [];
        return message;
    }
};
export const MsgUpdateResourceDependencyMappingProposalJsonFile = {
    $type: "cosmos.accesscontrol.v1beta1.MsgUpdateResourceDependencyMappingProposalJsonFile",
    encode(message, writer = new BinaryWriter()) {
        if (message.title !== "") {
            writer.uint32(10).string(message.title);
        }
        if (message.description !== "") {
            writer.uint32(18).string(message.description);
        }
        if (message.deposit !== "") {
            writer.uint32(26).string(message.deposit);
        }
        for (const v of message.message_dependency_mapping) {
            MessageDependencyMapping.encode(v, writer.uint32(34).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgUpdateResourceDependencyMappingProposalJsonFile();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.title = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.description = reader.string();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.deposit = reader.string();
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.message_dependency_mapping.push(MessageDependencyMapping.decode(reader, reader.uint32()));
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
            title: isSet(object.title) ? globalThis.String(object.title) : "",
            description: isSet(object.description) ? globalThis.String(object.description) : "",
            deposit: isSet(object.deposit) ? globalThis.String(object.deposit) : "",
            message_dependency_mapping: globalThis.Array.isArray(object?.message_dependency_mapping)
                ? object.message_dependency_mapping.map((e) => MessageDependencyMapping.fromJSON(e))
                : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.title !== "") {
            obj.title = message.title;
        }
        if (message.description !== "") {
            obj.description = message.description;
        }
        if (message.deposit !== "") {
            obj.deposit = message.deposit;
        }
        if (message.message_dependency_mapping?.length) {
            obj.message_dependency_mapping = message.message_dependency_mapping.map((e) => MessageDependencyMapping.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return MsgUpdateResourceDependencyMappingProposalJsonFile.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgUpdateResourceDependencyMappingProposalJsonFile();
        message.title = object.title ?? "";
        message.description = object.description ?? "";
        message.deposit = object.deposit ?? "";
        message.message_dependency_mapping = object.message_dependency_mapping?.map((e) => MessageDependencyMapping.fromPartial(e)) || [];
        return message;
    }
};
export const MsgUpdateResourceDependencyMappingProposalResponse = {
    $type: "cosmos.accesscontrol.v1beta1.MsgUpdateResourceDependencyMappingProposalResponse",
    encode(_, writer = new BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgUpdateResourceDependencyMappingProposalResponse();
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
        return MsgUpdateResourceDependencyMappingProposalResponse.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseMsgUpdateResourceDependencyMappingProposalResponse();
        return message;
    }
};
export const MsgUpdateWasmDependencyMappingProposal = {
    $type: "cosmos.accesscontrol.v1beta1.MsgUpdateWasmDependencyMappingProposal",
    encode(message, writer = new BinaryWriter()) {
        if (message.title !== "") {
            writer.uint32(10).string(message.title);
        }
        if (message.description !== "") {
            writer.uint32(18).string(message.description);
        }
        if (message.contract_address !== "") {
            writer.uint32(26).string(message.contract_address);
        }
        if (message.wasm_dependency_mapping !== undefined) {
            WasmDependencyMapping.encode(message.wasm_dependency_mapping, writer.uint32(34).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgUpdateWasmDependencyMappingProposal();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.title = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.description = reader.string();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.contract_address = reader.string();
                    continue;
                case 4:
                    if (tag !== 34) {
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
            title: isSet(object.title) ? globalThis.String(object.title) : "",
            description: isSet(object.description) ? globalThis.String(object.description) : "",
            contract_address: isSet(object.contract_address) ? globalThis.String(object.contract_address) : "",
            wasm_dependency_mapping: isSet(object.wasm_dependency_mapping) ? WasmDependencyMapping.fromJSON(object.wasm_dependency_mapping) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.title !== "") {
            obj.title = message.title;
        }
        if (message.description !== "") {
            obj.description = message.description;
        }
        if (message.contract_address !== "") {
            obj.contract_address = message.contract_address;
        }
        if (message.wasm_dependency_mapping !== undefined) {
            obj.wasm_dependency_mapping = WasmDependencyMapping.toJSON(message.wasm_dependency_mapping);
        }
        return obj;
    },
    create(base) {
        return MsgUpdateWasmDependencyMappingProposal.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgUpdateWasmDependencyMappingProposal();
        message.title = object.title ?? "";
        message.description = object.description ?? "";
        message.contract_address = object.contract_address ?? "";
        message.wasm_dependency_mapping =
            object.wasm_dependency_mapping !== undefined && object.wasm_dependency_mapping !== null
                ? WasmDependencyMapping.fromPartial(object.wasm_dependency_mapping)
                : undefined;
        return message;
    }
};
export const MsgUpdateWasmDependencyMappingProposalJsonFile = {
    $type: "cosmos.accesscontrol.v1beta1.MsgUpdateWasmDependencyMappingProposalJsonFile",
    encode(message, writer = new BinaryWriter()) {
        if (message.title !== "") {
            writer.uint32(10).string(message.title);
        }
        if (message.description !== "") {
            writer.uint32(18).string(message.description);
        }
        if (message.deposit !== "") {
            writer.uint32(26).string(message.deposit);
        }
        if (message.contract_address !== "") {
            writer.uint32(34).string(message.contract_address);
        }
        if (message.wasm_dependency_mapping !== undefined) {
            WasmDependencyMapping.encode(message.wasm_dependency_mapping, writer.uint32(42).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgUpdateWasmDependencyMappingProposalJsonFile();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.title = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.description = reader.string();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.deposit = reader.string();
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.contract_address = reader.string();
                    continue;
                case 5:
                    if (tag !== 42) {
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
            title: isSet(object.title) ? globalThis.String(object.title) : "",
            description: isSet(object.description) ? globalThis.String(object.description) : "",
            deposit: isSet(object.deposit) ? globalThis.String(object.deposit) : "",
            contract_address: isSet(object.contract_address) ? globalThis.String(object.contract_address) : "",
            wasm_dependency_mapping: isSet(object.wasm_dependency_mapping) ? WasmDependencyMapping.fromJSON(object.wasm_dependency_mapping) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.title !== "") {
            obj.title = message.title;
        }
        if (message.description !== "") {
            obj.description = message.description;
        }
        if (message.deposit !== "") {
            obj.deposit = message.deposit;
        }
        if (message.contract_address !== "") {
            obj.contract_address = message.contract_address;
        }
        if (message.wasm_dependency_mapping !== undefined) {
            obj.wasm_dependency_mapping = WasmDependencyMapping.toJSON(message.wasm_dependency_mapping);
        }
        return obj;
    },
    create(base) {
        return MsgUpdateWasmDependencyMappingProposalJsonFile.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgUpdateWasmDependencyMappingProposalJsonFile();
        message.title = object.title ?? "";
        message.description = object.description ?? "";
        message.deposit = object.deposit ?? "";
        message.contract_address = object.contract_address ?? "";
        message.wasm_dependency_mapping =
            object.wasm_dependency_mapping !== undefined && object.wasm_dependency_mapping !== null
                ? WasmDependencyMapping.fromPartial(object.wasm_dependency_mapping)
                : undefined;
        return message;
    }
};
function createBaseMsgUpdateResourceDependencyMappingProposal() {
    return { title: "", description: "", message_dependency_mapping: [] };
}
function createBaseMsgUpdateResourceDependencyMappingProposalJsonFile() {
    return { title: "", description: "", deposit: "", message_dependency_mapping: [] };
}
function createBaseMsgUpdateResourceDependencyMappingProposalResponse() {
    return {};
}
function createBaseMsgUpdateWasmDependencyMappingProposal() {
    return { title: "", description: "", contract_address: "", wasm_dependency_mapping: undefined };
}
function createBaseMsgUpdateWasmDependencyMappingProposalJsonFile() {
    return { title: "", description: "", deposit: "", contract_address: "", wasm_dependency_mapping: undefined };
}
function isSet(value) {
    return value !== null && value !== undefined;
}
export const registry = [
    ["/cosmos.accesscontrol.v1beta1.MsgUpdateResourceDependencyMappingProposal", MsgUpdateResourceDependencyMappingProposal],
    ["/cosmos.accesscontrol.v1beta1.MsgUpdateResourceDependencyMappingProposalJsonFile", MsgUpdateResourceDependencyMappingProposalJsonFile],
    ["/cosmos.accesscontrol.v1beta1.MsgUpdateResourceDependencyMappingProposalResponse", MsgUpdateResourceDependencyMappingProposalResponse],
    ["/cosmos.accesscontrol.v1beta1.MsgUpdateWasmDependencyMappingProposal", MsgUpdateWasmDependencyMappingProposal],
    ["/cosmos.accesscontrol.v1beta1.MsgUpdateWasmDependencyMappingProposalJsonFile", MsgUpdateWasmDependencyMappingProposalJsonFile]
];
export const aminoConverters = {
    "/cosmos.accesscontrol.v1beta1.MsgUpdateResourceDependencyMappingProposal": {
        aminoType: "cosmos-sdk/MsgUpdateResourceDependencyMappingProposal",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.accesscontrol.v1beta1.MsgUpdateResourceDependencyMappingProposalJsonFile": {
        aminoType: "cosmos-sdk/MsgUpdateResourceDependencyMappingProposalJsonFile",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.accesscontrol.v1beta1.MsgUpdateResourceDependencyMappingProposalResponse": {
        aminoType: "cosmos-sdk/MsgUpdateResourceDependencyMappingProposalResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.accesscontrol.v1beta1.MsgUpdateWasmDependencyMappingProposal": {
        aminoType: "cosmos-sdk/MsgUpdateWasmDependencyMappingProposal",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.accesscontrol.v1beta1.MsgUpdateWasmDependencyMappingProposalJsonFile": {
        aminoType: "cosmos-sdk/MsgUpdateWasmDependencyMappingProposalJsonFile",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
