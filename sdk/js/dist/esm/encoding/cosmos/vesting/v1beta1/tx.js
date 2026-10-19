import { BinaryReader, BinaryWriter } from "@bufbuild/protobuf/wire";
import { Coin } from "../../base/v1beta1/coin";
export const MsgCreateVestingAccount = {
    $type: "cosmos.vesting.v1beta1.MsgCreateVestingAccount",
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
        if (message.end_time !== 0) {
            writer.uint32(32).int64(message.end_time);
        }
        if (message.delayed !== false) {
            writer.uint32(40).bool(message.delayed);
        }
        if (message.admin !== "") {
            writer.uint32(50).string(message.admin);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgCreateVestingAccount();
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
                case 4:
                    if (tag !== 32) {
                        break;
                    }
                    message.end_time = longToNumber(reader.int64());
                    continue;
                case 5:
                    if (tag !== 40) {
                        break;
                    }
                    message.delayed = reader.bool();
                    continue;
                case 6:
                    if (tag !== 50) {
                        break;
                    }
                    message.admin = reader.string();
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
            amount: globalThis.Array.isArray(object?.amount) ? object.amount.map((e) => Coin.fromJSON(e)) : [],
            end_time: isSet(object.end_time) ? globalThis.Number(object.end_time) : 0,
            delayed: isSet(object.delayed) ? globalThis.Boolean(object.delayed) : false,
            admin: isSet(object.admin) ? globalThis.String(object.admin) : ""
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
        if (message.end_time !== 0) {
            obj.end_time = Math.round(message.end_time);
        }
        if (message.delayed !== false) {
            obj.delayed = message.delayed;
        }
        if (message.admin !== "") {
            obj.admin = message.admin;
        }
        return obj;
    },
    create(base) {
        return MsgCreateVestingAccount.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgCreateVestingAccount();
        message.from_address = object.from_address ?? "";
        message.to_address = object.to_address ?? "";
        message.amount = object.amount?.map((e) => Coin.fromPartial(e)) || [];
        message.end_time = object.end_time ?? 0;
        message.delayed = object.delayed ?? false;
        message.admin = object.admin ?? "";
        return message;
    }
};
export const MsgCreateVestingAccountResponse = {
    $type: "cosmos.vesting.v1beta1.MsgCreateVestingAccountResponse",
    encode(_, writer = new BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgCreateVestingAccountResponse();
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
        return MsgCreateVestingAccountResponse.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseMsgCreateVestingAccountResponse();
        return message;
    }
};
function createBaseMsgCreateVestingAccount() {
    return { from_address: "", to_address: "", amount: [], end_time: 0, delayed: false, admin: "" };
}
function createBaseMsgCreateVestingAccountResponse() {
    return {};
}
function longToNumber(int64) {
    const num = globalThis.Number(int64.toString());
    if (num > globalThis.Number.MAX_SAFE_INTEGER) {
        throw new globalThis.Error("Value is larger than Number.MAX_SAFE_INTEGER");
    }
    if (num < globalThis.Number.MIN_SAFE_INTEGER) {
        throw new globalThis.Error("Value is smaller than Number.MIN_SAFE_INTEGER");
    }
    return num;
}
function isSet(value) {
    return value !== null && value !== undefined;
}
export const registry = [
    ["/cosmos.vesting.v1beta1.MsgCreateVestingAccount", MsgCreateVestingAccount],
    ["/cosmos.vesting.v1beta1.MsgCreateVestingAccountResponse", MsgCreateVestingAccountResponse]
];
export const aminoConverters = {
    "/cosmos.vesting.v1beta1.MsgCreateVestingAccount": {
        aminoType: "cosmos-sdk/MsgCreateVestingAccount",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.vesting.v1beta1.MsgCreateVestingAccountResponse": {
        aminoType: "cosmos-sdk/MsgCreateVestingAccountResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
