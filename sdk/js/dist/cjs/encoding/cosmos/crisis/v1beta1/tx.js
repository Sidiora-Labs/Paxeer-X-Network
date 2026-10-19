"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.MsgVerifyInvariantResponse = exports.MsgVerifyInvariant = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
exports.MsgVerifyInvariant = {
    $type: "cosmos.crisis.v1beta1.MsgVerifyInvariant",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.sender !== "") {
            writer.uint32(10).string(message.sender);
        }
        if (message.invariant_module_name !== "") {
            writer.uint32(18).string(message.invariant_module_name);
        }
        if (message.invariant_route !== "") {
            writer.uint32(26).string(message.invariant_route);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgVerifyInvariant();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.sender = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.invariant_module_name = reader.string();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.invariant_route = reader.string();
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
            sender: isSet(object.sender) ? globalThis.String(object.sender) : "",
            invariant_module_name: isSet(object.invariant_module_name) ? globalThis.String(object.invariant_module_name) : "",
            invariant_route: isSet(object.invariant_route) ? globalThis.String(object.invariant_route) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.sender !== "") {
            obj.sender = message.sender;
        }
        if (message.invariant_module_name !== "") {
            obj.invariant_module_name = message.invariant_module_name;
        }
        if (message.invariant_route !== "") {
            obj.invariant_route = message.invariant_route;
        }
        return obj;
    },
    create(base) {
        return exports.MsgVerifyInvariant.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgVerifyInvariant();
        message.sender = object.sender ?? "";
        message.invariant_module_name = object.invariant_module_name ?? "";
        message.invariant_route = object.invariant_route ?? "";
        return message;
    }
};
exports.MsgVerifyInvariantResponse = {
    $type: "cosmos.crisis.v1beta1.MsgVerifyInvariantResponse",
    encode(_, writer = new wire_1.BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgVerifyInvariantResponse();
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
        return exports.MsgVerifyInvariantResponse.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseMsgVerifyInvariantResponse();
        return message;
    }
};
function createBaseMsgVerifyInvariant() {
    return { sender: "", invariant_module_name: "", invariant_route: "" };
}
function createBaseMsgVerifyInvariantResponse() {
    return {};
}
function isSet(value) {
    return value !== null && value !== undefined;
}
exports.registry = [
    ["/cosmos.crisis.v1beta1.MsgVerifyInvariant", exports.MsgVerifyInvariant],
    ["/cosmos.crisis.v1beta1.MsgVerifyInvariantResponse", exports.MsgVerifyInvariantResponse]
];
exports.aminoConverters = {
    "/cosmos.crisis.v1beta1.MsgVerifyInvariant": {
        aminoType: "cosmos-sdk/MsgVerifyInvariant",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.crisis.v1beta1.MsgVerifyInvariantResponse": {
        aminoType: "cosmos-sdk/MsgVerifyInvariantResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
