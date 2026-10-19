import { BinaryReader, BinaryWriter } from "@bufbuild/protobuf/wire";
import { Coin } from "../../base/v1beta1/coin";
import { Input, Output } from "./bank";
export const MsgSend = {
    $type: "cosmos.bank.v1beta1.MsgSend",
    encode(message, writer = new BinaryWriter()) {
        if (message.from_address !== "") {
            writer.uint32(10).string(message.from_address);
        }
        if (message.to_address !== "") {
            writer.uint32(18).string(message.to_address);
        }
        for (const v of message.amount) {
            Coin.encode(v, writer.uint32(26).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
                    message.amount.push(Coin.decode(reader, reader.uint32()));
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
            amount: globalThis.Array.isArray(object?.amount) ? object.amount.map((e) => Coin.fromJSON(e)) : []
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
            obj.amount = message.amount.map((e) => Coin.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return MsgSend.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgSend();
        message.from_address = object.from_address ?? "";
        message.to_address = object.to_address ?? "";
        message.amount = object.amount?.map((e) => Coin.fromPartial(e)) || [];
        return message;
    }
};
export const MsgSendResponse = {
    $type: "cosmos.bank.v1beta1.MsgSendResponse",
    encode(_, writer = new BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
        return MsgSendResponse.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseMsgSendResponse();
        return message;
    }
};
export const MsgMultiSend = {
    $type: "cosmos.bank.v1beta1.MsgMultiSend",
    encode(message, writer = new BinaryWriter()) {
        for (const v of message.inputs) {
            Input.encode(v, writer.uint32(10).fork()).join();
        }
        for (const v of message.outputs) {
            Output.encode(v, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgMultiSend();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.inputs.push(Input.decode(reader, reader.uint32()));
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.outputs.push(Output.decode(reader, reader.uint32()));
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
            inputs: globalThis.Array.isArray(object?.inputs) ? object.inputs.map((e) => Input.fromJSON(e)) : [],
            outputs: globalThis.Array.isArray(object?.outputs) ? object.outputs.map((e) => Output.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.inputs?.length) {
            obj.inputs = message.inputs.map((e) => Input.toJSON(e));
        }
        if (message.outputs?.length) {
            obj.outputs = message.outputs.map((e) => Output.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return MsgMultiSend.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgMultiSend();
        message.inputs = object.inputs?.map((e) => Input.fromPartial(e)) || [];
        message.outputs = object.outputs?.map((e) => Output.fromPartial(e)) || [];
        return message;
    }
};
export const MsgMultiSendResponse = {
    $type: "cosmos.bank.v1beta1.MsgMultiSendResponse",
    encode(_, writer = new BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
        return MsgMultiSendResponse.fromPartial(base ?? {});
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
export const registry = [
    ["/cosmos.bank.v1beta1.MsgSend", MsgSend],
    ["/cosmos.bank.v1beta1.MsgSendResponse", MsgSendResponse],
    ["/cosmos.bank.v1beta1.MsgMultiSend", MsgMultiSend],
    ["/cosmos.bank.v1beta1.MsgMultiSendResponse", MsgMultiSendResponse]
];
export const aminoConverters = {
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
