import { BinaryReader, BinaryWriter } from "@bufbuild/protobuf/wire";
import { Any } from "../../../google/protobuf/any";
import { Timestamp } from "../../../google/protobuf/timestamp";
import { Coin } from "../../base/v1beta1/coin";
import { CommissionRates, Description } from "./staking";
export const MsgCreateValidator = {
    $type: "cosmos.staking.v1beta1.MsgCreateValidator",
    encode(message, writer = new BinaryWriter()) {
        if (message.description !== undefined) {
            Description.encode(message.description, writer.uint32(10).fork()).join();
        }
        if (message.commission !== undefined) {
            CommissionRates.encode(message.commission, writer.uint32(18).fork()).join();
        }
        if (message.min_self_delegation !== "") {
            writer.uint32(26).string(message.min_self_delegation);
        }
        if (message.delegator_address !== "") {
            writer.uint32(34).string(message.delegator_address);
        }
        if (message.validator_address !== "") {
            writer.uint32(42).string(message.validator_address);
        }
        if (message.pubkey !== undefined) {
            Any.encode(message.pubkey, writer.uint32(50).fork()).join();
        }
        if (message.value !== undefined) {
            Coin.encode(message.value, writer.uint32(58).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgCreateValidator();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.description = Description.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.commission = CommissionRates.decode(reader, reader.uint32());
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.min_self_delegation = reader.string();
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.delegator_address = reader.string();
                    continue;
                case 5:
                    if (tag !== 42) {
                        break;
                    }
                    message.validator_address = reader.string();
                    continue;
                case 6:
                    if (tag !== 50) {
                        break;
                    }
                    message.pubkey = Any.decode(reader, reader.uint32());
                    continue;
                case 7:
                    if (tag !== 58) {
                        break;
                    }
                    message.value = Coin.decode(reader, reader.uint32());
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
            description: isSet(object.description) ? Description.fromJSON(object.description) : undefined,
            commission: isSet(object.commission) ? CommissionRates.fromJSON(object.commission) : undefined,
            min_self_delegation: isSet(object.min_self_delegation) ? globalThis.String(object.min_self_delegation) : "",
            delegator_address: isSet(object.delegator_address) ? globalThis.String(object.delegator_address) : "",
            validator_address: isSet(object.validator_address) ? globalThis.String(object.validator_address) : "",
            pubkey: isSet(object.pubkey) ? Any.fromJSON(object.pubkey) : undefined,
            value: isSet(object.value) ? Coin.fromJSON(object.value) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.description !== undefined) {
            obj.description = Description.toJSON(message.description);
        }
        if (message.commission !== undefined) {
            obj.commission = CommissionRates.toJSON(message.commission);
        }
        if (message.min_self_delegation !== "") {
            obj.min_self_delegation = message.min_self_delegation;
        }
        if (message.delegator_address !== "") {
            obj.delegator_address = message.delegator_address;
        }
        if (message.validator_address !== "") {
            obj.validator_address = message.validator_address;
        }
        if (message.pubkey !== undefined) {
            obj.pubkey = Any.toJSON(message.pubkey);
        }
        if (message.value !== undefined) {
            obj.value = Coin.toJSON(message.value);
        }
        return obj;
    },
    create(base) {
        return MsgCreateValidator.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgCreateValidator();
        message.description = object.description !== undefined && object.description !== null ? Description.fromPartial(object.description) : undefined;
        message.commission = object.commission !== undefined && object.commission !== null ? CommissionRates.fromPartial(object.commission) : undefined;
        message.min_self_delegation = object.min_self_delegation ?? "";
        message.delegator_address = object.delegator_address ?? "";
        message.validator_address = object.validator_address ?? "";
        message.pubkey = object.pubkey !== undefined && object.pubkey !== null ? Any.fromPartial(object.pubkey) : undefined;
        message.value = object.value !== undefined && object.value !== null ? Coin.fromPartial(object.value) : undefined;
        return message;
    }
};
export const MsgCreateValidatorResponse = {
    $type: "cosmos.staking.v1beta1.MsgCreateValidatorResponse",
    encode(_, writer = new BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgCreateValidatorResponse();
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
        return MsgCreateValidatorResponse.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseMsgCreateValidatorResponse();
        return message;
    }
};
export const MsgEditValidator = {
    $type: "cosmos.staking.v1beta1.MsgEditValidator",
    encode(message, writer = new BinaryWriter()) {
        if (message.description !== undefined) {
            Description.encode(message.description, writer.uint32(10).fork()).join();
        }
        if (message.validator_address !== "") {
            writer.uint32(18).string(message.validator_address);
        }
        if (message.commission_rate !== "") {
            writer.uint32(26).string(message.commission_rate);
        }
        if (message.min_self_delegation !== "") {
            writer.uint32(34).string(message.min_self_delegation);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgEditValidator();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.description = Description.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.validator_address = reader.string();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.commission_rate = reader.string();
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.min_self_delegation = reader.string();
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
            description: isSet(object.description) ? Description.fromJSON(object.description) : undefined,
            validator_address: isSet(object.validator_address) ? globalThis.String(object.validator_address) : "",
            commission_rate: isSet(object.commission_rate) ? globalThis.String(object.commission_rate) : "",
            min_self_delegation: isSet(object.min_self_delegation) ? globalThis.String(object.min_self_delegation) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.description !== undefined) {
            obj.description = Description.toJSON(message.description);
        }
        if (message.validator_address !== "") {
            obj.validator_address = message.validator_address;
        }
        if (message.commission_rate !== "") {
            obj.commission_rate = message.commission_rate;
        }
        if (message.min_self_delegation !== "") {
            obj.min_self_delegation = message.min_self_delegation;
        }
        return obj;
    },
    create(base) {
        return MsgEditValidator.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgEditValidator();
        message.description = object.description !== undefined && object.description !== null ? Description.fromPartial(object.description) : undefined;
        message.validator_address = object.validator_address ?? "";
        message.commission_rate = object.commission_rate ?? "";
        message.min_self_delegation = object.min_self_delegation ?? "";
        return message;
    }
};
export const MsgEditValidatorResponse = {
    $type: "cosmos.staking.v1beta1.MsgEditValidatorResponse",
    encode(_, writer = new BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgEditValidatorResponse();
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
        return MsgEditValidatorResponse.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseMsgEditValidatorResponse();
        return message;
    }
};
export const MsgDelegate = {
    $type: "cosmos.staking.v1beta1.MsgDelegate",
    encode(message, writer = new BinaryWriter()) {
        if (message.delegator_address !== "") {
            writer.uint32(10).string(message.delegator_address);
        }
        if (message.validator_address !== "") {
            writer.uint32(18).string(message.validator_address);
        }
        if (message.amount !== undefined) {
            Coin.encode(message.amount, writer.uint32(26).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgDelegate();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.delegator_address = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.validator_address = reader.string();
                    continue;
                case 3:
                    if (tag !== 26) {
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
            delegator_address: isSet(object.delegator_address) ? globalThis.String(object.delegator_address) : "",
            validator_address: isSet(object.validator_address) ? globalThis.String(object.validator_address) : "",
            amount: isSet(object.amount) ? Coin.fromJSON(object.amount) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.delegator_address !== "") {
            obj.delegator_address = message.delegator_address;
        }
        if (message.validator_address !== "") {
            obj.validator_address = message.validator_address;
        }
        if (message.amount !== undefined) {
            obj.amount = Coin.toJSON(message.amount);
        }
        return obj;
    },
    create(base) {
        return MsgDelegate.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgDelegate();
        message.delegator_address = object.delegator_address ?? "";
        message.validator_address = object.validator_address ?? "";
        message.amount = object.amount !== undefined && object.amount !== null ? Coin.fromPartial(object.amount) : undefined;
        return message;
    }
};
export const MsgDelegateResponse = {
    $type: "cosmos.staking.v1beta1.MsgDelegateResponse",
    encode(_, writer = new BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgDelegateResponse();
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
        return MsgDelegateResponse.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseMsgDelegateResponse();
        return message;
    }
};
export const MsgBeginRedelegate = {
    $type: "cosmos.staking.v1beta1.MsgBeginRedelegate",
    encode(message, writer = new BinaryWriter()) {
        if (message.delegator_address !== "") {
            writer.uint32(10).string(message.delegator_address);
        }
        if (message.validator_src_address !== "") {
            writer.uint32(18).string(message.validator_src_address);
        }
        if (message.validator_dst_address !== "") {
            writer.uint32(26).string(message.validator_dst_address);
        }
        if (message.amount !== undefined) {
            Coin.encode(message.amount, writer.uint32(34).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgBeginRedelegate();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.delegator_address = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.validator_src_address = reader.string();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.validator_dst_address = reader.string();
                    continue;
                case 4:
                    if (tag !== 34) {
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
            delegator_address: isSet(object.delegator_address) ? globalThis.String(object.delegator_address) : "",
            validator_src_address: isSet(object.validator_src_address) ? globalThis.String(object.validator_src_address) : "",
            validator_dst_address: isSet(object.validator_dst_address) ? globalThis.String(object.validator_dst_address) : "",
            amount: isSet(object.amount) ? Coin.fromJSON(object.amount) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.delegator_address !== "") {
            obj.delegator_address = message.delegator_address;
        }
        if (message.validator_src_address !== "") {
            obj.validator_src_address = message.validator_src_address;
        }
        if (message.validator_dst_address !== "") {
            obj.validator_dst_address = message.validator_dst_address;
        }
        if (message.amount !== undefined) {
            obj.amount = Coin.toJSON(message.amount);
        }
        return obj;
    },
    create(base) {
        return MsgBeginRedelegate.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgBeginRedelegate();
        message.delegator_address = object.delegator_address ?? "";
        message.validator_src_address = object.validator_src_address ?? "";
        message.validator_dst_address = object.validator_dst_address ?? "";
        message.amount = object.amount !== undefined && object.amount !== null ? Coin.fromPartial(object.amount) : undefined;
        return message;
    }
};
export const MsgBeginRedelegateResponse = {
    $type: "cosmos.staking.v1beta1.MsgBeginRedelegateResponse",
    encode(message, writer = new BinaryWriter()) {
        if (message.completion_time !== undefined) {
            Timestamp.encode(toTimestamp(message.completion_time), writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgBeginRedelegateResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.completion_time = fromTimestamp(Timestamp.decode(reader, reader.uint32()));
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
        return { completion_time: isSet(object.completion_time) ? fromJsonTimestamp(object.completion_time) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.completion_time !== undefined) {
            obj.completion_time = message.completion_time.toISOString();
        }
        return obj;
    },
    create(base) {
        return MsgBeginRedelegateResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgBeginRedelegateResponse();
        message.completion_time = object.completion_time ?? undefined;
        return message;
    }
};
export const MsgUndelegate = {
    $type: "cosmos.staking.v1beta1.MsgUndelegate",
    encode(message, writer = new BinaryWriter()) {
        if (message.delegator_address !== "") {
            writer.uint32(10).string(message.delegator_address);
        }
        if (message.validator_address !== "") {
            writer.uint32(18).string(message.validator_address);
        }
        if (message.amount !== undefined) {
            Coin.encode(message.amount, writer.uint32(26).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgUndelegate();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.delegator_address = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.validator_address = reader.string();
                    continue;
                case 3:
                    if (tag !== 26) {
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
            delegator_address: isSet(object.delegator_address) ? globalThis.String(object.delegator_address) : "",
            validator_address: isSet(object.validator_address) ? globalThis.String(object.validator_address) : "",
            amount: isSet(object.amount) ? Coin.fromJSON(object.amount) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.delegator_address !== "") {
            obj.delegator_address = message.delegator_address;
        }
        if (message.validator_address !== "") {
            obj.validator_address = message.validator_address;
        }
        if (message.amount !== undefined) {
            obj.amount = Coin.toJSON(message.amount);
        }
        return obj;
    },
    create(base) {
        return MsgUndelegate.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgUndelegate();
        message.delegator_address = object.delegator_address ?? "";
        message.validator_address = object.validator_address ?? "";
        message.amount = object.amount !== undefined && object.amount !== null ? Coin.fromPartial(object.amount) : undefined;
        return message;
    }
};
export const MsgUndelegateResponse = {
    $type: "cosmos.staking.v1beta1.MsgUndelegateResponse",
    encode(message, writer = new BinaryWriter()) {
        if (message.completion_time !== undefined) {
            Timestamp.encode(toTimestamp(message.completion_time), writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgUndelegateResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.completion_time = fromTimestamp(Timestamp.decode(reader, reader.uint32()));
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
        return { completion_time: isSet(object.completion_time) ? fromJsonTimestamp(object.completion_time) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.completion_time !== undefined) {
            obj.completion_time = message.completion_time.toISOString();
        }
        return obj;
    },
    create(base) {
        return MsgUndelegateResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgUndelegateResponse();
        message.completion_time = object.completion_time ?? undefined;
        return message;
    }
};
function createBaseMsgCreateValidator() {
    return {
        description: undefined,
        commission: undefined,
        min_self_delegation: "",
        delegator_address: "",
        validator_address: "",
        pubkey: undefined,
        value: undefined
    };
}
function createBaseMsgCreateValidatorResponse() {
    return {};
}
function createBaseMsgEditValidator() {
    return { description: undefined, validator_address: "", commission_rate: "", min_self_delegation: "" };
}
function createBaseMsgEditValidatorResponse() {
    return {};
}
function createBaseMsgDelegate() {
    return { delegator_address: "", validator_address: "", amount: undefined };
}
function createBaseMsgDelegateResponse() {
    return {};
}
function createBaseMsgBeginRedelegate() {
    return { delegator_address: "", validator_src_address: "", validator_dst_address: "", amount: undefined };
}
function createBaseMsgBeginRedelegateResponse() {
    return { completion_time: undefined };
}
function createBaseMsgUndelegate() {
    return { delegator_address: "", validator_address: "", amount: undefined };
}
function createBaseMsgUndelegateResponse() {
    return { completion_time: undefined };
}
function toTimestamp(date) {
    const seconds = Math.trunc(date.getTime() / 1000);
    const nanos = (date.getTime() % 1000) * 1000000;
    return { seconds, nanos };
}
function fromTimestamp(t) {
    let millis = (t.seconds || 0) * 1000;
    millis += (t.nanos || 0) / 1000000;
    return new globalThis.Date(millis);
}
function fromJsonTimestamp(o) {
    if (o instanceof globalThis.Date) {
        return o;
    }
    else if (typeof o === "string") {
        return new globalThis.Date(o);
    }
    else {
        return fromTimestamp(Timestamp.fromJSON(o));
    }
}
function isSet(value) {
    return value !== null && value !== undefined;
}
export const registry = [
    ["/cosmos.staking.v1beta1.MsgCreateValidator", MsgCreateValidator],
    ["/cosmos.staking.v1beta1.MsgCreateValidatorResponse", MsgCreateValidatorResponse],
    ["/cosmos.staking.v1beta1.MsgEditValidator", MsgEditValidator],
    ["/cosmos.staking.v1beta1.MsgEditValidatorResponse", MsgEditValidatorResponse],
    ["/cosmos.staking.v1beta1.MsgDelegate", MsgDelegate],
    ["/cosmos.staking.v1beta1.MsgDelegateResponse", MsgDelegateResponse],
    ["/cosmos.staking.v1beta1.MsgBeginRedelegate", MsgBeginRedelegate],
    ["/cosmos.staking.v1beta1.MsgBeginRedelegateResponse", MsgBeginRedelegateResponse],
    ["/cosmos.staking.v1beta1.MsgUndelegate", MsgUndelegate],
    ["/cosmos.staking.v1beta1.MsgUndelegateResponse", MsgUndelegateResponse]
];
export const aminoConverters = {
    "/cosmos.staking.v1beta1.MsgCreateValidator": {
        aminoType: "cosmos-sdk/MsgCreateValidator",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.staking.v1beta1.MsgCreateValidatorResponse": {
        aminoType: "cosmos-sdk/MsgCreateValidatorResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.staking.v1beta1.MsgEditValidator": {
        aminoType: "cosmos-sdk/MsgEditValidator",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.staking.v1beta1.MsgEditValidatorResponse": {
        aminoType: "cosmos-sdk/MsgEditValidatorResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.staking.v1beta1.MsgDelegate": {
        aminoType: "cosmos-sdk/MsgDelegate",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.staking.v1beta1.MsgDelegateResponse": {
        aminoType: "cosmos-sdk/MsgDelegateResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.staking.v1beta1.MsgBeginRedelegate": {
        aminoType: "cosmos-sdk/MsgBeginRedelegate",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.staking.v1beta1.MsgBeginRedelegateResponse": {
        aminoType: "cosmos-sdk/MsgBeginRedelegateResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.staking.v1beta1.MsgUndelegate": {
        aminoType: "cosmos-sdk/MsgUndelegate",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.staking.v1beta1.MsgUndelegateResponse": {
        aminoType: "cosmos-sdk/MsgUndelegateResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
