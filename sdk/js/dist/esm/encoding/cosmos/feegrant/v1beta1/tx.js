import { BinaryReader, BinaryWriter } from "@bufbuild/protobuf/wire";
import { Any } from "../../../google/protobuf/any";
export const MsgGrantAllowance = {
    $type: "cosmos.feegrant.v1beta1.MsgGrantAllowance",
    encode(message, writer = new BinaryWriter()) {
        if (message.granter !== "") {
            writer.uint32(10).string(message.granter);
        }
        if (message.grantee !== "") {
            writer.uint32(18).string(message.grantee);
        }
        if (message.allowance !== undefined) {
            Any.encode(message.allowance, writer.uint32(26).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgGrantAllowance();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.granter = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.grantee = reader.string();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.allowance = Any.decode(reader, reader.uint32());
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
            granter: isSet(object.granter) ? globalThis.String(object.granter) : "",
            grantee: isSet(object.grantee) ? globalThis.String(object.grantee) : "",
            allowance: isSet(object.allowance) ? Any.fromJSON(object.allowance) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.granter !== "") {
            obj.granter = message.granter;
        }
        if (message.grantee !== "") {
            obj.grantee = message.grantee;
        }
        if (message.allowance !== undefined) {
            obj.allowance = Any.toJSON(message.allowance);
        }
        return obj;
    },
    create(base) {
        return MsgGrantAllowance.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgGrantAllowance();
        message.granter = object.granter ?? "";
        message.grantee = object.grantee ?? "";
        message.allowance = object.allowance !== undefined && object.allowance !== null ? Any.fromPartial(object.allowance) : undefined;
        return message;
    }
};
export const MsgGrantAllowanceResponse = {
    $type: "cosmos.feegrant.v1beta1.MsgGrantAllowanceResponse",
    encode(_, writer = new BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgGrantAllowanceResponse();
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
        return MsgGrantAllowanceResponse.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseMsgGrantAllowanceResponse();
        return message;
    }
};
export const MsgRevokeAllowance = {
    $type: "cosmos.feegrant.v1beta1.MsgRevokeAllowance",
    encode(message, writer = new BinaryWriter()) {
        if (message.granter !== "") {
            writer.uint32(10).string(message.granter);
        }
        if (message.grantee !== "") {
            writer.uint32(18).string(message.grantee);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgRevokeAllowance();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.granter = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.grantee = reader.string();
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
            granter: isSet(object.granter) ? globalThis.String(object.granter) : "",
            grantee: isSet(object.grantee) ? globalThis.String(object.grantee) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.granter !== "") {
            obj.granter = message.granter;
        }
        if (message.grantee !== "") {
            obj.grantee = message.grantee;
        }
        return obj;
    },
    create(base) {
        return MsgRevokeAllowance.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgRevokeAllowance();
        message.granter = object.granter ?? "";
        message.grantee = object.grantee ?? "";
        return message;
    }
};
export const MsgRevokeAllowanceResponse = {
    $type: "cosmos.feegrant.v1beta1.MsgRevokeAllowanceResponse",
    encode(_, writer = new BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgRevokeAllowanceResponse();
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
        return MsgRevokeAllowanceResponse.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseMsgRevokeAllowanceResponse();
        return message;
    }
};
function createBaseMsgGrantAllowance() {
    return { granter: "", grantee: "", allowance: undefined };
}
function createBaseMsgGrantAllowanceResponse() {
    return {};
}
function createBaseMsgRevokeAllowance() {
    return { granter: "", grantee: "" };
}
function createBaseMsgRevokeAllowanceResponse() {
    return {};
}
function isSet(value) {
    return value !== null && value !== undefined;
}
export const registry = [
    ["/cosmos.feegrant.v1beta1.MsgGrantAllowance", MsgGrantAllowance],
    ["/cosmos.feegrant.v1beta1.MsgGrantAllowanceResponse", MsgGrantAllowanceResponse],
    ["/cosmos.feegrant.v1beta1.MsgRevokeAllowance", MsgRevokeAllowance],
    ["/cosmos.feegrant.v1beta1.MsgRevokeAllowanceResponse", MsgRevokeAllowanceResponse]
];
export const aminoConverters = {
    "/cosmos.feegrant.v1beta1.MsgGrantAllowance": {
        aminoType: "cosmos-sdk/MsgGrantAllowance",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.feegrant.v1beta1.MsgGrantAllowanceResponse": {
        aminoType: "cosmos-sdk/MsgGrantAllowanceResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.feegrant.v1beta1.MsgRevokeAllowance": {
        aminoType: "cosmos-sdk/MsgRevokeAllowance",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.feegrant.v1beta1.MsgRevokeAllowanceResponse": {
        aminoType: "cosmos-sdk/MsgRevokeAllowanceResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
