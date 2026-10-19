"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.WasmDependencyMapping = exports.MessageDependencyMapping = exports.WasmAccessOperations = exports.WasmContractReferences = exports.WasmContractReference = exports.WasmAccessOperation = exports.AccessOperation = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
const constants_1 = require("./constants");
exports.AccessOperation = {
    $type: "cosmos.accesscontrol.v1beta1.AccessOperation",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.access_type !== 0) {
            writer.uint32(8).int32(message.access_type);
        }
        if (message.resource_type !== 0) {
            writer.uint32(16).int32(message.resource_type);
        }
        if (message.identifier_template !== "") {
            writer.uint32(26).string(message.identifier_template);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseAccessOperation();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.access_type = reader.int32();
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.resource_type = reader.int32();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.identifier_template = reader.string();
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
            access_type: isSet(object.access_type) ? (0, constants_1.accessTypeFromJSON)(object.access_type) : 0,
            resource_type: isSet(object.resource_type) ? (0, constants_1.resourceTypeFromJSON)(object.resource_type) : 0,
            identifier_template: isSet(object.identifier_template) ? globalThis.String(object.identifier_template) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.access_type !== 0) {
            obj.access_type = (0, constants_1.accessTypeToJSON)(message.access_type);
        }
        if (message.resource_type !== 0) {
            obj.resource_type = (0, constants_1.resourceTypeToJSON)(message.resource_type);
        }
        if (message.identifier_template !== "") {
            obj.identifier_template = message.identifier_template;
        }
        return obj;
    },
    create(base) {
        return exports.AccessOperation.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseAccessOperation();
        message.access_type = object.access_type ?? 0;
        message.resource_type = object.resource_type ?? 0;
        message.identifier_template = object.identifier_template ?? "";
        return message;
    }
};
exports.WasmAccessOperation = {
    $type: "cosmos.accesscontrol.v1beta1.WasmAccessOperation",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.operation !== undefined) {
            exports.AccessOperation.encode(message.operation, writer.uint32(10).fork()).join();
        }
        if (message.selector_type !== 0) {
            writer.uint32(16).int32(message.selector_type);
        }
        if (message.selector !== "") {
            writer.uint32(26).string(message.selector);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseWasmAccessOperation();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.operation = exports.AccessOperation.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.selector_type = reader.int32();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.selector = reader.string();
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
            operation: isSet(object.operation) ? exports.AccessOperation.fromJSON(object.operation) : undefined,
            selector_type: isSet(object.selector_type) ? (0, constants_1.accessOperationSelectorTypeFromJSON)(object.selector_type) : 0,
            selector: isSet(object.selector) ? globalThis.String(object.selector) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.operation !== undefined) {
            obj.operation = exports.AccessOperation.toJSON(message.operation);
        }
        if (message.selector_type !== 0) {
            obj.selector_type = (0, constants_1.accessOperationSelectorTypeToJSON)(message.selector_type);
        }
        if (message.selector !== "") {
            obj.selector = message.selector;
        }
        return obj;
    },
    create(base) {
        return exports.WasmAccessOperation.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseWasmAccessOperation();
        message.operation = object.operation !== undefined && object.operation !== null ? exports.AccessOperation.fromPartial(object.operation) : undefined;
        message.selector_type = object.selector_type ?? 0;
        message.selector = object.selector ?? "";
        return message;
    }
};
exports.WasmContractReference = {
    $type: "cosmos.accesscontrol.v1beta1.WasmContractReference",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.contract_address !== "") {
            writer.uint32(10).string(message.contract_address);
        }
        if (message.message_type !== 0) {
            writer.uint32(16).int32(message.message_type);
        }
        if (message.message_name !== "") {
            writer.uint32(26).string(message.message_name);
        }
        if (message.json_translation_template !== "") {
            writer.uint32(34).string(message.json_translation_template);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseWasmContractReference();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.contract_address = reader.string();
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.message_type = reader.int32();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.message_name = reader.string();
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.json_translation_template = reader.string();
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
            contract_address: isSet(object.contract_address) ? globalThis.String(object.contract_address) : "",
            message_type: isSet(object.message_type) ? (0, constants_1.wasmMessageSubtypeFromJSON)(object.message_type) : 0,
            message_name: isSet(object.message_name) ? globalThis.String(object.message_name) : "",
            json_translation_template: isSet(object.json_translation_template) ? globalThis.String(object.json_translation_template) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.contract_address !== "") {
            obj.contract_address = message.contract_address;
        }
        if (message.message_type !== 0) {
            obj.message_type = (0, constants_1.wasmMessageSubtypeToJSON)(message.message_type);
        }
        if (message.message_name !== "") {
            obj.message_name = message.message_name;
        }
        if (message.json_translation_template !== "") {
            obj.json_translation_template = message.json_translation_template;
        }
        return obj;
    },
    create(base) {
        return exports.WasmContractReference.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseWasmContractReference();
        message.contract_address = object.contract_address ?? "";
        message.message_type = object.message_type ?? 0;
        message.message_name = object.message_name ?? "";
        message.json_translation_template = object.json_translation_template ?? "";
        return message;
    }
};
exports.WasmContractReferences = {
    $type: "cosmos.accesscontrol.v1beta1.WasmContractReferences",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.message_name !== "") {
            writer.uint32(10).string(message.message_name);
        }
        for (const v of message.contract_references) {
            exports.WasmContractReference.encode(v, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseWasmContractReferences();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.message_name = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.contract_references.push(exports.WasmContractReference.decode(reader, reader.uint32()));
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
            message_name: isSet(object.message_name) ? globalThis.String(object.message_name) : "",
            contract_references: globalThis.Array.isArray(object?.contract_references)
                ? object.contract_references.map((e) => exports.WasmContractReference.fromJSON(e))
                : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.message_name !== "") {
            obj.message_name = message.message_name;
        }
        if (message.contract_references?.length) {
            obj.contract_references = message.contract_references.map((e) => exports.WasmContractReference.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.WasmContractReferences.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseWasmContractReferences();
        message.message_name = object.message_name ?? "";
        message.contract_references = object.contract_references?.map((e) => exports.WasmContractReference.fromPartial(e)) || [];
        return message;
    }
};
exports.WasmAccessOperations = {
    $type: "cosmos.accesscontrol.v1beta1.WasmAccessOperations",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.message_name !== "") {
            writer.uint32(10).string(message.message_name);
        }
        for (const v of message.wasm_operations) {
            exports.WasmAccessOperation.encode(v, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseWasmAccessOperations();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.message_name = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.wasm_operations.push(exports.WasmAccessOperation.decode(reader, reader.uint32()));
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
            message_name: isSet(object.message_name) ? globalThis.String(object.message_name) : "",
            wasm_operations: globalThis.Array.isArray(object?.wasm_operations) ? object.wasm_operations.map((e) => exports.WasmAccessOperation.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.message_name !== "") {
            obj.message_name = message.message_name;
        }
        if (message.wasm_operations?.length) {
            obj.wasm_operations = message.wasm_operations.map((e) => exports.WasmAccessOperation.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.WasmAccessOperations.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseWasmAccessOperations();
        message.message_name = object.message_name ?? "";
        message.wasm_operations = object.wasm_operations?.map((e) => exports.WasmAccessOperation.fromPartial(e)) || [];
        return message;
    }
};
exports.MessageDependencyMapping = {
    $type: "cosmos.accesscontrol.v1beta1.MessageDependencyMapping",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.message_key !== "") {
            writer.uint32(10).string(message.message_key);
        }
        for (const v of message.access_ops) {
            exports.AccessOperation.encode(v, writer.uint32(18).fork()).join();
        }
        if (message.dynamic_enabled !== false) {
            writer.uint32(24).bool(message.dynamic_enabled);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMessageDependencyMapping();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.message_key = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.access_ops.push(exports.AccessOperation.decode(reader, reader.uint32()));
                    continue;
                case 3:
                    if (tag !== 24) {
                        break;
                    }
                    message.dynamic_enabled = reader.bool();
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
            message_key: isSet(object.message_key) ? globalThis.String(object.message_key) : "",
            access_ops: globalThis.Array.isArray(object?.access_ops) ? object.access_ops.map((e) => exports.AccessOperation.fromJSON(e)) : [],
            dynamic_enabled: isSet(object.dynamic_enabled) ? globalThis.Boolean(object.dynamic_enabled) : false
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.message_key !== "") {
            obj.message_key = message.message_key;
        }
        if (message.access_ops?.length) {
            obj.access_ops = message.access_ops.map((e) => exports.AccessOperation.toJSON(e));
        }
        if (message.dynamic_enabled !== false) {
            obj.dynamic_enabled = message.dynamic_enabled;
        }
        return obj;
    },
    create(base) {
        return exports.MessageDependencyMapping.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMessageDependencyMapping();
        message.message_key = object.message_key ?? "";
        message.access_ops = object.access_ops?.map((e) => exports.AccessOperation.fromPartial(e)) || [];
        message.dynamic_enabled = object.dynamic_enabled ?? false;
        return message;
    }
};
exports.WasmDependencyMapping = {
    $type: "cosmos.accesscontrol.v1beta1.WasmDependencyMapping",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.base_access_ops) {
            exports.WasmAccessOperation.encode(v, writer.uint32(10).fork()).join();
        }
        for (const v of message.query_access_ops) {
            exports.WasmAccessOperations.encode(v, writer.uint32(18).fork()).join();
        }
        for (const v of message.execute_access_ops) {
            exports.WasmAccessOperations.encode(v, writer.uint32(26).fork()).join();
        }
        for (const v of message.base_contract_references) {
            exports.WasmContractReference.encode(v, writer.uint32(34).fork()).join();
        }
        for (const v of message.query_contract_references) {
            exports.WasmContractReferences.encode(v, writer.uint32(42).fork()).join();
        }
        for (const v of message.execute_contract_references) {
            exports.WasmContractReferences.encode(v, writer.uint32(50).fork()).join();
        }
        if (message.reset_reason !== "") {
            writer.uint32(58).string(message.reset_reason);
        }
        if (message.contract_address !== "") {
            writer.uint32(66).string(message.contract_address);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseWasmDependencyMapping();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.base_access_ops.push(exports.WasmAccessOperation.decode(reader, reader.uint32()));
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.query_access_ops.push(exports.WasmAccessOperations.decode(reader, reader.uint32()));
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.execute_access_ops.push(exports.WasmAccessOperations.decode(reader, reader.uint32()));
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.base_contract_references.push(exports.WasmContractReference.decode(reader, reader.uint32()));
                    continue;
                case 5:
                    if (tag !== 42) {
                        break;
                    }
                    message.query_contract_references.push(exports.WasmContractReferences.decode(reader, reader.uint32()));
                    continue;
                case 6:
                    if (tag !== 50) {
                        break;
                    }
                    message.execute_contract_references.push(exports.WasmContractReferences.decode(reader, reader.uint32()));
                    continue;
                case 7:
                    if (tag !== 58) {
                        break;
                    }
                    message.reset_reason = reader.string();
                    continue;
                case 8:
                    if (tag !== 66) {
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
        return {
            base_access_ops: globalThis.Array.isArray(object?.base_access_ops) ? object.base_access_ops.map((e) => exports.WasmAccessOperation.fromJSON(e)) : [],
            query_access_ops: globalThis.Array.isArray(object?.query_access_ops) ? object.query_access_ops.map((e) => exports.WasmAccessOperations.fromJSON(e)) : [],
            execute_access_ops: globalThis.Array.isArray(object?.execute_access_ops)
                ? object.execute_access_ops.map((e) => exports.WasmAccessOperations.fromJSON(e))
                : [],
            base_contract_references: globalThis.Array.isArray(object?.base_contract_references)
                ? object.base_contract_references.map((e) => exports.WasmContractReference.fromJSON(e))
                : [],
            query_contract_references: globalThis.Array.isArray(object?.query_contract_references)
                ? object.query_contract_references.map((e) => exports.WasmContractReferences.fromJSON(e))
                : [],
            execute_contract_references: globalThis.Array.isArray(object?.execute_contract_references)
                ? object.execute_contract_references.map((e) => exports.WasmContractReferences.fromJSON(e))
                : [],
            reset_reason: isSet(object.reset_reason) ? globalThis.String(object.reset_reason) : "",
            contract_address: isSet(object.contract_address) ? globalThis.String(object.contract_address) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.base_access_ops?.length) {
            obj.base_access_ops = message.base_access_ops.map((e) => exports.WasmAccessOperation.toJSON(e));
        }
        if (message.query_access_ops?.length) {
            obj.query_access_ops = message.query_access_ops.map((e) => exports.WasmAccessOperations.toJSON(e));
        }
        if (message.execute_access_ops?.length) {
            obj.execute_access_ops = message.execute_access_ops.map((e) => exports.WasmAccessOperations.toJSON(e));
        }
        if (message.base_contract_references?.length) {
            obj.base_contract_references = message.base_contract_references.map((e) => exports.WasmContractReference.toJSON(e));
        }
        if (message.query_contract_references?.length) {
            obj.query_contract_references = message.query_contract_references.map((e) => exports.WasmContractReferences.toJSON(e));
        }
        if (message.execute_contract_references?.length) {
            obj.execute_contract_references = message.execute_contract_references.map((e) => exports.WasmContractReferences.toJSON(e));
        }
        if (message.reset_reason !== "") {
            obj.reset_reason = message.reset_reason;
        }
        if (message.contract_address !== "") {
            obj.contract_address = message.contract_address;
        }
        return obj;
    },
    create(base) {
        return exports.WasmDependencyMapping.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseWasmDependencyMapping();
        message.base_access_ops = object.base_access_ops?.map((e) => exports.WasmAccessOperation.fromPartial(e)) || [];
        message.query_access_ops = object.query_access_ops?.map((e) => exports.WasmAccessOperations.fromPartial(e)) || [];
        message.execute_access_ops = object.execute_access_ops?.map((e) => exports.WasmAccessOperations.fromPartial(e)) || [];
        message.base_contract_references = object.base_contract_references?.map((e) => exports.WasmContractReference.fromPartial(e)) || [];
        message.query_contract_references = object.query_contract_references?.map((e) => exports.WasmContractReferences.fromPartial(e)) || [];
        message.execute_contract_references = object.execute_contract_references?.map((e) => exports.WasmContractReferences.fromPartial(e)) || [];
        message.reset_reason = object.reset_reason ?? "";
        message.contract_address = object.contract_address ?? "";
        return message;
    }
};
function createBaseAccessOperation() {
    return { access_type: 0, resource_type: 0, identifier_template: "" };
}
function createBaseWasmAccessOperation() {
    return { operation: undefined, selector_type: 0, selector: "" };
}
function createBaseWasmContractReference() {
    return { contract_address: "", message_type: 0, message_name: "", json_translation_template: "" };
}
function createBaseWasmContractReferences() {
    return { message_name: "", contract_references: [] };
}
function createBaseWasmAccessOperations() {
    return { message_name: "", wasm_operations: [] };
}
function createBaseMessageDependencyMapping() {
    return { message_key: "", access_ops: [], dynamic_enabled: false };
}
function createBaseWasmDependencyMapping() {
    return {
        base_access_ops: [],
        query_access_ops: [],
        execute_access_ops: [],
        base_contract_references: [],
        query_contract_references: [],
        execute_contract_references: [],
        reset_reason: "",
        contract_address: ""
    };
}
function isSet(value) {
    return value !== null && value !== undefined;
}
exports.registry = [
    ["/cosmos.accesscontrol.v1beta1.AccessOperation", exports.AccessOperation],
    ["/cosmos.accesscontrol.v1beta1.WasmAccessOperation", exports.WasmAccessOperation],
    ["/cosmos.accesscontrol.v1beta1.WasmContractReference", exports.WasmContractReference],
    ["/cosmos.accesscontrol.v1beta1.WasmContractReferences", exports.WasmContractReferences],
    ["/cosmos.accesscontrol.v1beta1.WasmAccessOperations", exports.WasmAccessOperations],
    ["/cosmos.accesscontrol.v1beta1.MessageDependencyMapping", exports.MessageDependencyMapping],
    ["/cosmos.accesscontrol.v1beta1.WasmDependencyMapping", exports.WasmDependencyMapping]
];
exports.aminoConverters = {
    "/cosmos.accesscontrol.v1beta1.AccessOperation": {
        aminoType: "cosmos-sdk/AccessOperation",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.accesscontrol.v1beta1.WasmAccessOperation": {
        aminoType: "cosmos-sdk/WasmAccessOperation",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.accesscontrol.v1beta1.WasmContractReference": {
        aminoType: "cosmos-sdk/WasmContractReference",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.accesscontrol.v1beta1.WasmContractReferences": {
        aminoType: "cosmos-sdk/WasmContractReferences",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.accesscontrol.v1beta1.WasmAccessOperations": {
        aminoType: "cosmos-sdk/WasmAccessOperations",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.accesscontrol.v1beta1.MessageDependencyMapping": {
        aminoType: "cosmos-sdk/MessageDependencyMapping",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.accesscontrol.v1beta1.WasmDependencyMapping": {
        aminoType: "cosmos-sdk/WasmDependencyMapping",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
