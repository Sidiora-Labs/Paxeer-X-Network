import { BinaryReader, BinaryWriter } from "@bufbuild/protobuf/wire";
import { AllowList, Metadata } from "../cosmos/bank/v1beta1/bank";
import { Coin } from "../cosmos/base/v1beta1/coin";
export const MsgCreateDenom = {
    $type: "paxprotocol.paxchain.tokenfactory.MsgCreateDenom",
    encode(message, writer = new BinaryWriter()) {
        if (message.sender !== "") {
            writer.uint32(10).string(message.sender);
        }
        if (message.subdenom !== "") {
            writer.uint32(18).string(message.subdenom);
        }
        if (message.allow_list !== undefined) {
            AllowList.encode(message.allow_list, writer.uint32(26).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgCreateDenom();
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
                    message.subdenom = reader.string();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.allow_list = AllowList.decode(reader, reader.uint32());
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
            subdenom: isSet(object.subdenom) ? globalThis.String(object.subdenom) : "",
            allow_list: isSet(object.allow_list) ? AllowList.fromJSON(object.allow_list) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.sender !== "") {
            obj.sender = message.sender;
        }
        if (message.subdenom !== "") {
            obj.subdenom = message.subdenom;
        }
        if (message.allow_list !== undefined) {
            obj.allow_list = AllowList.toJSON(message.allow_list);
        }
        return obj;
    },
    create(base) {
        return MsgCreateDenom.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgCreateDenom();
        message.sender = object.sender ?? "";
        message.subdenom = object.subdenom ?? "";
        message.allow_list = object.allow_list !== undefined && object.allow_list !== null ? AllowList.fromPartial(object.allow_list) : undefined;
        return message;
    }
};
export const MsgCreateDenomResponse = {
    $type: "paxprotocol.paxchain.tokenfactory.MsgCreateDenomResponse",
    encode(message, writer = new BinaryWriter()) {
        if (message.new_token_denom !== "") {
            writer.uint32(10).string(message.new_token_denom);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgCreateDenomResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.new_token_denom = reader.string();
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
        return { new_token_denom: isSet(object.new_token_denom) ? globalThis.String(object.new_token_denom) : "" };
    },
    toJSON(message) {
        const obj = {};
        if (message.new_token_denom !== "") {
            obj.new_token_denom = message.new_token_denom;
        }
        return obj;
    },
    create(base) {
        return MsgCreateDenomResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgCreateDenomResponse();
        message.new_token_denom = object.new_token_denom ?? "";
        return message;
    }
};
export const MsgMint = {
    $type: "paxprotocol.paxchain.tokenfactory.MsgMint",
    encode(message, writer = new BinaryWriter()) {
        if (message.sender !== "") {
            writer.uint32(10).string(message.sender);
        }
        if (message.amount !== undefined) {
            Coin.encode(message.amount, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgMint();
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
                    message.amount = Coin.decode(reader, reader.uint32());
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
            amount: isSet(object.amount) ? Coin.fromJSON(object.amount) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.sender !== "") {
            obj.sender = message.sender;
        }
        if (message.amount !== undefined) {
            obj.amount = Coin.toJSON(message.amount);
        }
        return obj;
    },
    create(base) {
        return MsgMint.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgMint();
        message.sender = object.sender ?? "";
        message.amount = object.amount !== undefined && object.amount !== null ? Coin.fromPartial(object.amount) : undefined;
        return message;
    }
};
export const MsgMintResponse = {
    $type: "paxprotocol.paxchain.tokenfactory.MsgMintResponse",
    encode(_, writer = new BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgMintResponse();
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
        return MsgMintResponse.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseMsgMintResponse();
        return message;
    }
};
export const MsgBurn = {
    $type: "paxprotocol.paxchain.tokenfactory.MsgBurn",
    encode(message, writer = new BinaryWriter()) {
        if (message.sender !== "") {
            writer.uint32(10).string(message.sender);
        }
        if (message.amount !== undefined) {
            Coin.encode(message.amount, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgBurn();
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
                    message.amount = Coin.decode(reader, reader.uint32());
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
            amount: isSet(object.amount) ? Coin.fromJSON(object.amount) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.sender !== "") {
            obj.sender = message.sender;
        }
        if (message.amount !== undefined) {
            obj.amount = Coin.toJSON(message.amount);
        }
        return obj;
    },
    create(base) {
        return MsgBurn.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgBurn();
        message.sender = object.sender ?? "";
        message.amount = object.amount !== undefined && object.amount !== null ? Coin.fromPartial(object.amount) : undefined;
        return message;
    }
};
export const MsgBurnResponse = {
    $type: "paxprotocol.paxchain.tokenfactory.MsgBurnResponse",
    encode(_, writer = new BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgBurnResponse();
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
        return MsgBurnResponse.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseMsgBurnResponse();
        return message;
    }
};
export const MsgChangeAdmin = {
    $type: "paxprotocol.paxchain.tokenfactory.MsgChangeAdmin",
    encode(message, writer = new BinaryWriter()) {
        if (message.sender !== "") {
            writer.uint32(10).string(message.sender);
        }
        if (message.denom !== "") {
            writer.uint32(18).string(message.denom);
        }
        if (message.new_admin !== "") {
            writer.uint32(26).string(message.new_admin);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgChangeAdmin();
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
                    message.denom = reader.string();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.new_admin = reader.string();
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
            denom: isSet(object.denom) ? globalThis.String(object.denom) : "",
            new_admin: isSet(object.new_admin) ? globalThis.String(object.new_admin) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.sender !== "") {
            obj.sender = message.sender;
        }
        if (message.denom !== "") {
            obj.denom = message.denom;
        }
        if (message.new_admin !== "") {
            obj.new_admin = message.new_admin;
        }
        return obj;
    },
    create(base) {
        return MsgChangeAdmin.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgChangeAdmin();
        message.sender = object.sender ?? "";
        message.denom = object.denom ?? "";
        message.new_admin = object.new_admin ?? "";
        return message;
    }
};
export const MsgChangeAdminResponse = {
    $type: "paxprotocol.paxchain.tokenfactory.MsgChangeAdminResponse",
    encode(_, writer = new BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgChangeAdminResponse();
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
        return MsgChangeAdminResponse.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseMsgChangeAdminResponse();
        return message;
    }
};
export const MsgSetDenomMetadata = {
    $type: "paxprotocol.paxchain.tokenfactory.MsgSetDenomMetadata",
    encode(message, writer = new BinaryWriter()) {
        if (message.sender !== "") {
            writer.uint32(10).string(message.sender);
        }
        if (message.metadata !== undefined) {
            Metadata.encode(message.metadata, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgSetDenomMetadata();
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
                    message.metadata = Metadata.decode(reader, reader.uint32());
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
            metadata: isSet(object.metadata) ? Metadata.fromJSON(object.metadata) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.sender !== "") {
            obj.sender = message.sender;
        }
        if (message.metadata !== undefined) {
            obj.metadata = Metadata.toJSON(message.metadata);
        }
        return obj;
    },
    create(base) {
        return MsgSetDenomMetadata.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgSetDenomMetadata();
        message.sender = object.sender ?? "";
        message.metadata = object.metadata !== undefined && object.metadata !== null ? Metadata.fromPartial(object.metadata) : undefined;
        return message;
    }
};
export const MsgSetDenomMetadataResponse = {
    $type: "paxprotocol.paxchain.tokenfactory.MsgSetDenomMetadataResponse",
    encode(_, writer = new BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgSetDenomMetadataResponse();
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
        return MsgSetDenomMetadataResponse.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseMsgSetDenomMetadataResponse();
        return message;
    }
};
export const MsgUpdateDenom = {
    $type: "paxprotocol.paxchain.tokenfactory.MsgUpdateDenom",
    encode(message, writer = new BinaryWriter()) {
        if (message.sender !== "") {
            writer.uint32(10).string(message.sender);
        }
        if (message.denom !== "") {
            writer.uint32(18).string(message.denom);
        }
        if (message.allow_list !== undefined) {
            AllowList.encode(message.allow_list, writer.uint32(26).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgUpdateDenom();
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
                    message.denom = reader.string();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.allow_list = AllowList.decode(reader, reader.uint32());
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
            denom: isSet(object.denom) ? globalThis.String(object.denom) : "",
            allow_list: isSet(object.allow_list) ? AllowList.fromJSON(object.allow_list) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.sender !== "") {
            obj.sender = message.sender;
        }
        if (message.denom !== "") {
            obj.denom = message.denom;
        }
        if (message.allow_list !== undefined) {
            obj.allow_list = AllowList.toJSON(message.allow_list);
        }
        return obj;
    },
    create(base) {
        return MsgUpdateDenom.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgUpdateDenom();
        message.sender = object.sender ?? "";
        message.denom = object.denom ?? "";
        message.allow_list = object.allow_list !== undefined && object.allow_list !== null ? AllowList.fromPartial(object.allow_list) : undefined;
        return message;
    }
};
export const MsgUpdateDenomResponse = {
    $type: "paxprotocol.paxchain.tokenfactory.MsgUpdateDenomResponse",
    encode(_, writer = new BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgUpdateDenomResponse();
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
        return MsgUpdateDenomResponse.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseMsgUpdateDenomResponse();
        return message;
    }
};
function createBaseMsgCreateDenom() {
    return { sender: "", subdenom: "", allow_list: undefined };
}
function createBaseMsgCreateDenomResponse() {
    return { new_token_denom: "" };
}
function createBaseMsgMint() {
    return { sender: "", amount: undefined };
}
function createBaseMsgMintResponse() {
    return {};
}
function createBaseMsgBurn() {
    return { sender: "", amount: undefined };
}
function createBaseMsgBurnResponse() {
    return {};
}
function createBaseMsgChangeAdmin() {
    return { sender: "", denom: "", new_admin: "" };
}
function createBaseMsgChangeAdminResponse() {
    return {};
}
function createBaseMsgSetDenomMetadata() {
    return { sender: "", metadata: undefined };
}
function createBaseMsgSetDenomMetadataResponse() {
    return {};
}
function createBaseMsgUpdateDenom() {
    return { sender: "", denom: "", allow_list: undefined };
}
function createBaseMsgUpdateDenomResponse() {
    return {};
}
function isSet(value) {
    return value !== null && value !== undefined;
}
export const registry = [
    ["/paxprotocol.paxchain.tokenfactory.MsgCreateDenom", MsgCreateDenom],
    ["/paxprotocol.paxchain.tokenfactory.MsgCreateDenomResponse", MsgCreateDenomResponse],
    ["/paxprotocol.paxchain.tokenfactory.MsgMint", MsgMint],
    ["/paxprotocol.paxchain.tokenfactory.MsgMintResponse", MsgMintResponse],
    ["/paxprotocol.paxchain.tokenfactory.MsgBurn", MsgBurn],
    ["/paxprotocol.paxchain.tokenfactory.MsgBurnResponse", MsgBurnResponse],
    ["/paxprotocol.paxchain.tokenfactory.MsgChangeAdmin", MsgChangeAdmin],
    ["/paxprotocol.paxchain.tokenfactory.MsgChangeAdminResponse", MsgChangeAdminResponse],
    ["/paxprotocol.paxchain.tokenfactory.MsgSetDenomMetadata", MsgSetDenomMetadata],
    ["/paxprotocol.paxchain.tokenfactory.MsgSetDenomMetadataResponse", MsgSetDenomMetadataResponse],
    ["/paxprotocol.paxchain.tokenfactory.MsgUpdateDenom", MsgUpdateDenom],
    ["/paxprotocol.paxchain.tokenfactory.MsgUpdateDenomResponse", MsgUpdateDenomResponse]
];
export const aminoConverters = {
    "/paxprotocol.paxchain.tokenfactory.MsgCreateDenom": {
        aminoType: "tokenfactory/MsgCreateDenom",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.tokenfactory.MsgCreateDenomResponse": {
        aminoType: "tokenfactory/MsgCreateDenomResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.tokenfactory.MsgMint": {
        aminoType: "tokenfactory/MsgMint",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.tokenfactory.MsgMintResponse": {
        aminoType: "tokenfactory/MsgMintResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.tokenfactory.MsgBurn": {
        aminoType: "tokenfactory/MsgBurn",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.tokenfactory.MsgBurnResponse": {
        aminoType: "tokenfactory/MsgBurnResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.tokenfactory.MsgChangeAdmin": {
        aminoType: "tokenfactory/MsgChangeAdmin",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.tokenfactory.MsgChangeAdminResponse": {
        aminoType: "tokenfactory/MsgChangeAdminResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.tokenfactory.MsgSetDenomMetadata": {
        aminoType: "tokenfactory/MsgSetDenomMetadata",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.tokenfactory.MsgSetDenomMetadataResponse": {
        aminoType: "tokenfactory/MsgSetDenomMetadataResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.tokenfactory.MsgUpdateDenom": {
        aminoType: "tokenfactory/MsgUpdateDenom",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.tokenfactory.MsgUpdateDenomResponse": {
        aminoType: "tokenfactory/MsgUpdateDenomResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
