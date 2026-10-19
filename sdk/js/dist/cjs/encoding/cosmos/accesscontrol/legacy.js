"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.LegacyWasmDependencyMapping = exports.LegacyAccessOperationWithSelector = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
const accesscontrol_1 = require("./accesscontrol");
const constants_1 = require("./constants");
exports.LegacyAccessOperationWithSelector = {
    $type: "cosmos.accesscontrol.v1beta1.LegacyAccessOperationWithSelector",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.operation !== undefined) {
            accesscontrol_1.AccessOperation.encode(message.operation, writer.uint32(10).fork()).join();
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
        const message = createBaseLegacyAccessOperationWithSelector();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.operation = accesscontrol_1.AccessOperation.decode(reader, reader.uint32());
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
            operation: isSet(object.operation) ? accesscontrol_1.AccessOperation.fromJSON(object.operation) : undefined,
            selector_type: isSet(object.selector_type) ? (0, constants_1.accessOperationSelectorTypeFromJSON)(object.selector_type) : 0,
            selector: isSet(object.selector) ? globalThis.String(object.selector) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.operation !== undefined) {
            obj.operation = accesscontrol_1.AccessOperation.toJSON(message.operation);
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
        return exports.LegacyAccessOperationWithSelector.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseLegacyAccessOperationWithSelector();
        message.operation = object.operation !== undefined && object.operation !== null ? accesscontrol_1.AccessOperation.fromPartial(object.operation) : undefined;
        message.selector_type = object.selector_type ?? 0;
        message.selector = object.selector ?? "";
        return message;
    }
};
exports.LegacyWasmDependencyMapping = {
    $type: "cosmos.accesscontrol.v1beta1.LegacyWasmDependencyMapping",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.enabled !== false) {
            writer.uint32(8).bool(message.enabled);
        }
        for (const v of message.access_ops) {
            exports.LegacyAccessOperationWithSelector.encode(v, writer.uint32(18).fork()).join();
        }
        if (message.reset_reason !== "") {
            writer.uint32(26).string(message.reset_reason);
        }
        if (message.contract_address !== "") {
            writer.uint32(34).string(message.contract_address);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseLegacyWasmDependencyMapping();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.enabled = reader.bool();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.access_ops.push(exports.LegacyAccessOperationWithSelector.decode(reader, reader.uint32()));
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.reset_reason = reader.string();
                    continue;
                case 4:
                    if (tag !== 34) {
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
            enabled: isSet(object.enabled) ? globalThis.Boolean(object.enabled) : false,
            access_ops: globalThis.Array.isArray(object?.access_ops) ? object.access_ops.map((e) => exports.LegacyAccessOperationWithSelector.fromJSON(e)) : [],
            reset_reason: isSet(object.reset_reason) ? globalThis.String(object.reset_reason) : "",
            contract_address: isSet(object.contract_address) ? globalThis.String(object.contract_address) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.enabled !== false) {
            obj.enabled = message.enabled;
        }
        if (message.access_ops?.length) {
            obj.access_ops = message.access_ops.map((e) => exports.LegacyAccessOperationWithSelector.toJSON(e));
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
        return exports.LegacyWasmDependencyMapping.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseLegacyWasmDependencyMapping();
        message.enabled = object.enabled ?? false;
        message.access_ops = object.access_ops?.map((e) => exports.LegacyAccessOperationWithSelector.fromPartial(e)) || [];
        message.reset_reason = object.reset_reason ?? "";
        message.contract_address = object.contract_address ?? "";
        return message;
    }
};
function createBaseLegacyAccessOperationWithSelector() {
    return { operation: undefined, selector_type: 0, selector: "" };
}
function createBaseLegacyWasmDependencyMapping() {
    return { enabled: false, access_ops: [], reset_reason: "", contract_address: "" };
}
function isSet(value) {
    return value !== null && value !== undefined;
}
exports.registry = [
    ["/cosmos.accesscontrol.v1beta1.LegacyAccessOperationWithSelector", exports.LegacyAccessOperationWithSelector],
    ["/cosmos.accesscontrol.v1beta1.LegacyWasmDependencyMapping", exports.LegacyWasmDependencyMapping]
];
exports.aminoConverters = {
    "/cosmos.accesscontrol.v1beta1.LegacyAccessOperationWithSelector": {
        aminoType: "cosmos-sdk/LegacyAccessOperationWithSelector",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.accesscontrol.v1beta1.LegacyWasmDependencyMapping": {
        aminoType: "cosmos-sdk/LegacyWasmDependencyMapping",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
