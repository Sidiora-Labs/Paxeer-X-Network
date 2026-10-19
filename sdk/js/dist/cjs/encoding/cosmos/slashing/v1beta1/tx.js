"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.MsgUnjailResponse = exports.MsgUnjail = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
exports.MsgUnjail = {
    $type: "cosmos.slashing.v1beta1.MsgUnjail",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.validator_addr !== "") {
            writer.uint32(10).string(message.validator_addr);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgUnjail();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.validator_addr = reader.string();
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
        return { validator_addr: isSet(object.validator_addr) ? globalThis.String(object.validator_addr) : "" };
    },
    toJSON(message) {
        const obj = {};
        if (message.validator_addr !== "") {
            obj.validator_addr = message.validator_addr;
        }
        return obj;
    },
    create(base) {
        return exports.MsgUnjail.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgUnjail();
        message.validator_addr = object.validator_addr ?? "";
        return message;
    }
};
exports.MsgUnjailResponse = {
    $type: "cosmos.slashing.v1beta1.MsgUnjailResponse",
    encode(_, writer = new wire_1.BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgUnjailResponse();
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
        return exports.MsgUnjailResponse.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseMsgUnjailResponse();
        return message;
    }
};
function createBaseMsgUnjail() {
    return { validator_addr: "" };
}
function createBaseMsgUnjailResponse() {
    return {};
}
function isSet(value) {
    return value !== null && value !== undefined;
}
exports.registry = [
    ["/cosmos.slashing.v1beta1.MsgUnjail", exports.MsgUnjail],
    ["/cosmos.slashing.v1beta1.MsgUnjailResponse", exports.MsgUnjailResponse]
];
exports.aminoConverters = {
    "/cosmos.slashing.v1beta1.MsgUnjail": {
        aminoType: "cosmos-sdk/MsgUnjail",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.slashing.v1beta1.MsgUnjailResponse": {
        aminoType: "cosmos-sdk/MsgUnjailResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
