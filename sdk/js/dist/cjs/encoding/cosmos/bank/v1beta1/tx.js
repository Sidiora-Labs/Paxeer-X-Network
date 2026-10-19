"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.MsgMultiSendResponse = exports.MsgMultiSend = exports.MsgSendResponse = exports.MsgSend = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
const coin_1 = require("../../base/v1beta1/coin");
const bank_1 = require("./bank");
exports.MsgSend = {
    $type: "cosmos.bank.v1beta1.MsgSend",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.from_address !== "") {
            writer.uint32(10).string(message.from_address);
        }
        if (message.to_address !== "") {
            writer.uint32(18).string(message.to_address);
        }
        for (const v of message.amount) {
            coin_1.Coin.encode(v, writer.uint32(26).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgSend();
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
                    message.to_address = reader.string();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.amount.push(coin_1.Coin.decode(reader, reader.uint32()));
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
            to_address: isSet(object.to_address) ? globalThis.String(object.to_address) : "",
            amount: globalThis.Array.isArray(object?.amount) ? object.amount.map((e) => coin_1.Coin.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.from_address !== "") {
            obj.from_address = message.from_address;
        }
        if (message.to_address !== "") {
            obj.to_address = message.to_address;
        }
        if (message.amount?.length) {
            obj.amount = message.amount.map((e) => coin_1.Coin.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.MsgSend.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgSend();
        message.from_address = object.from_address ?? "";
        message.to_address = object.to_address ?? "";
        message.amount = object.amount?.map((e) => coin_1.Coin.fromPartial(e)) || [];
        return message;
    }
};
exports.MsgSendResponse = {
    $type: "cosmos.bank.v1beta1.MsgSendResponse",
    encode(_, writer = new wire_1.BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgSendResponse();
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
        return exports.MsgSendResponse.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseMsgSendResponse();
        return message;
    }
};
exports.MsgMultiSend = {
    $type: "cosmos.bank.v1beta1.MsgMultiSend",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.inputs) {
            bank_1.Input.encode(v, writer.uint32(10).fork()).join();
        }
        for (const v of message.outputs) {
            bank_1.Output.encode(v, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgMultiSend();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.inputs.push(bank_1.Input.decode(reader, reader.uint32()));
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.outputs.push(bank_1.Output.decode(reader, reader.uint32()));
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
            inputs: globalThis.Array.isArray(object?.inputs) ? object.inputs.map((e) => bank_1.Input.fromJSON(e)) : [],
            outputs: globalThis.Array.isArray(object?.outputs) ? object.outputs.map((e) => bank_1.Output.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.inputs?.length) {
            obj.inputs = message.inputs.map((e) => bank_1.Input.toJSON(e));
        }
        if (message.outputs?.length) {
            obj.outputs = message.outputs.map((e) => bank_1.Output.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.MsgMultiSend.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgMultiSend();
        message.inputs = object.inputs?.map((e) => bank_1.Input.fromPartial(e)) || [];
        message.outputs = object.outputs?.map((e) => bank_1.Output.fromPartial(e)) || [];
        return message;
    }
};
exports.MsgMultiSendResponse = {
    $type: "cosmos.bank.v1beta1.MsgMultiSendResponse",
    encode(_, writer = new wire_1.BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgMultiSendResponse();
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
        return exports.MsgMultiSendResponse.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseMsgMultiSendResponse();
        return message;
    }
};
function createBaseMsgSend() {
    return { from_address: "", to_address: "", amount: [] };
}
function createBaseMsgSendResponse() {
    return {};
}
function createBaseMsgMultiSend() {
    return { inputs: [], outputs: [] };
}
function createBaseMsgMultiSendResponse() {
    return {};
}
function isSet(value) {
    return value !== null && value !== undefined;
}
exports.registry = [
    ["/cosmos.bank.v1beta1.MsgSend", exports.MsgSend],
    ["/cosmos.bank.v1beta1.MsgSendResponse", exports.MsgSendResponse],
    ["/cosmos.bank.v1beta1.MsgMultiSend", exports.MsgMultiSend],
    ["/cosmos.bank.v1beta1.MsgMultiSendResponse", exports.MsgMultiSendResponse]
];
exports.aminoConverters = {
    "/cosmos.bank.v1beta1.MsgSend": {
        aminoType: "cosmos-sdk/MsgSend",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.bank.v1beta1.MsgSendResponse": {
        aminoType: "cosmos-sdk/MsgSendResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.bank.v1beta1.MsgMultiSend": {
        aminoType: "cosmos-sdk/MsgMultiSend",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.bank.v1beta1.MsgMultiSendResponse": {
        aminoType: "cosmos-sdk/MsgMultiSendResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
