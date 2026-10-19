"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.ParamChange = exports.ParameterChangeProposal = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
exports.ParameterChangeProposal = {
    $type: "cosmos.params.v1beta1.ParameterChangeProposal",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.title !== "") {
            writer.uint32(10).string(message.title);
        }
        if (message.description !== "") {
            writer.uint32(18).string(message.description);
        }
        for (const v of message.changes) {
            exports.ParamChange.encode(v, writer.uint32(26).fork()).join();
        }
        if (message.is_expedited !== false) {
            writer.uint32(32).bool(message.is_expedited);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseParameterChangeProposal();
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
                    message.changes.push(exports.ParamChange.decode(reader, reader.uint32()));
                    continue;
                case 4:
                    if (tag !== 32) {
                        break;
                    }
                    message.is_expedited = reader.bool();
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
            changes: globalThis.Array.isArray(object?.changes) ? object.changes.map((e) => exports.ParamChange.fromJSON(e)) : [],
            is_expedited: isSet(object.is_expedited) ? globalThis.Boolean(object.is_expedited) : false
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
        if (message.changes?.length) {
            obj.changes = message.changes.map((e) => exports.ParamChange.toJSON(e));
        }
        if (message.is_expedited !== false) {
            obj.is_expedited = message.is_expedited;
        }
        return obj;
    },
    create(base) {
        return exports.ParameterChangeProposal.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseParameterChangeProposal();
        message.title = object.title ?? "";
        message.description = object.description ?? "";
        message.changes = object.changes?.map((e) => exports.ParamChange.fromPartial(e)) || [];
        message.is_expedited = object.is_expedited ?? false;
        return message;
    }
};
exports.ParamChange = {
    $type: "cosmos.params.v1beta1.ParamChange",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.subspace !== "") {
            writer.uint32(10).string(message.subspace);
        }
        if (message.key !== "") {
            writer.uint32(18).string(message.key);
        }
        if (message.value !== "") {
            writer.uint32(26).string(message.value);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseParamChange();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.subspace = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.key = reader.string();
                    continue;
                case 3:
                    if (tag !== 26) {
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
            subspace: isSet(object.subspace) ? globalThis.String(object.subspace) : "",
            key: isSet(object.key) ? globalThis.String(object.key) : "",
            value: isSet(object.value) ? globalThis.String(object.value) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.subspace !== "") {
            obj.subspace = message.subspace;
        }
        if (message.key !== "") {
            obj.key = message.key;
        }
        if (message.value !== "") {
            obj.value = message.value;
        }
        return obj;
    },
    create(base) {
        return exports.ParamChange.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseParamChange();
        message.subspace = object.subspace ?? "";
        message.key = object.key ?? "";
        message.value = object.value ?? "";
        return message;
    }
};
function createBaseParameterChangeProposal() {
    return { title: "", description: "", changes: [], is_expedited: false };
}
function createBaseParamChange() {
    return { subspace: "", key: "", value: "" };
}
function isSet(value) {
    return value !== null && value !== undefined;
}
exports.registry = [
    ["/cosmos.params.v1beta1.ParameterChangeProposal", exports.ParameterChangeProposal],
    ["/cosmos.params.v1beta1.ParamChange", exports.ParamChange]
];
exports.aminoConverters = {
    "/cosmos.params.v1beta1.ParameterChangeProposal": {
        aminoType: "cosmos-sdk/ParameterChangeProposal",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.params.v1beta1.ParamChange": {
        aminoType: "cosmos-sdk/ParamChange",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
