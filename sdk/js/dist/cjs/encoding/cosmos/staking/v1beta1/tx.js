"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.MsgUndelegateResponse = exports.MsgUndelegate = exports.MsgBeginRedelegateResponse = exports.MsgBeginRedelegate = exports.MsgDelegateResponse = exports.MsgDelegate = exports.MsgEditValidatorResponse = exports.MsgEditValidator = exports.MsgCreateValidatorResponse = exports.MsgCreateValidator = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
const any_1 = require("../../../google/protobuf/any");
const timestamp_1 = require("../../../google/protobuf/timestamp");
const coin_1 = require("../../base/v1beta1/coin");
const staking_1 = require("./staking");
exports.MsgCreateValidator = {
    $type: "cosmos.staking.v1beta1.MsgCreateValidator",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.description !== undefined) {
            staking_1.Description.encode(message.description, writer.uint32(10).fork()).join();
        }
        if (message.commission !== undefined) {
            staking_1.CommissionRates.encode(message.commission, writer.uint32(18).fork()).join();
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
            any_1.Any.encode(message.pubkey, writer.uint32(50).fork()).join();
        }
        if (message.value !== undefined) {
            coin_1.Coin.encode(message.value, writer.uint32(58).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgCreateValidator();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.description = staking_1.Description.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.commission = staking_1.CommissionRates.decode(reader, reader.uint32());
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
                    message.pubkey = any_1.Any.decode(reader, reader.uint32());
                    continue;
                case 7:
                    if (tag !== 58) {
                        break;
                    }
                    message.value = coin_1.Coin.decode(reader, reader.uint32());
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
            description: isSet(object.description) ? staking_1.Description.fromJSON(object.description) : undefined,
            commission: isSet(object.commission) ? staking_1.CommissionRates.fromJSON(object.commission) : undefined,
            min_self_delegation: isSet(object.min_self_delegation) ? globalThis.String(object.min_self_delegation) : "",
            delegator_address: isSet(object.delegator_address) ? globalThis.String(object.delegator_address) : "",
            validator_address: isSet(object.validator_address) ? globalThis.String(object.validator_address) : "",
            pubkey: isSet(object.pubkey) ? any_1.Any.fromJSON(object.pubkey) : undefined,
            value: isSet(object.value) ? coin_1.Coin.fromJSON(object.value) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.description !== undefined) {
            obj.description = staking_1.Description.toJSON(message.description);
        }
        if (message.commission !== undefined) {
            obj.commission = staking_1.CommissionRates.toJSON(message.commission);
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
            obj.pubkey = any_1.Any.toJSON(message.pubkey);
        }
        if (message.value !== undefined) {
            obj.value = coin_1.Coin.toJSON(message.value);
        }
        return obj;
    },
    create(base) {
        return exports.MsgCreateValidator.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgCreateValidator();
        message.description = object.description !== undefined && object.description !== null ? staking_1.Description.fromPartial(object.description) : undefined;
        message.commission = object.commission !== undefined && object.commission !== null ? staking_1.CommissionRates.fromPartial(object.commission) : undefined;
        message.min_self_delegation = object.min_self_delegation ?? "";
        message.delegator_address = object.delegator_address ?? "";
        message.validator_address = object.validator_address ?? "";
        message.pubkey = object.pubkey !== undefined && object.pubkey !== null ? any_1.Any.fromPartial(object.pubkey) : undefined;
        message.value = object.value !== undefined && object.value !== null ? coin_1.Coin.fromPartial(object.value) : undefined;
        return message;
    }
};
exports.MsgCreateValidatorResponse = {
    $type: "cosmos.staking.v1beta1.MsgCreateValidatorResponse",
    encode(_, writer = new wire_1.BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
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
        return exports.MsgCreateValidatorResponse.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseMsgCreateValidatorResponse();
        return message;
    }
};
exports.MsgEditValidator = {
    $type: "cosmos.staking.v1beta1.MsgEditValidator",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.description !== undefined) {
            staking_1.Description.encode(message.description, writer.uint32(10).fork()).join();
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
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgEditValidator();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.description = staking_1.Description.decode(reader, reader.uint32());
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
            description: isSet(object.description) ? staking_1.Description.fromJSON(object.description) : undefined,
            validator_address: isSet(object.validator_address) ? globalThis.String(object.validator_address) : "",
            commission_rate: isSet(object.commission_rate) ? globalThis.String(object.commission_rate) : "",
            min_self_delegation: isSet(object.min_self_delegation) ? globalThis.String(object.min_self_delegation) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.description !== undefined) {
            obj.description = staking_1.Description.toJSON(message.description);
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
        return exports.MsgEditValidator.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgEditValidator();
        message.description = object.description !== undefined && object.description !== null ? staking_1.Description.fromPartial(object.description) : undefined;
        message.validator_address = object.validator_address ?? "";
        message.commission_rate = object.commission_rate ?? "";
        message.min_self_delegation = object.min_self_delegation ?? "";
        return message;
    }
};
exports.MsgEditValidatorResponse = {
    $type: "cosmos.staking.v1beta1.MsgEditValidatorResponse",
    encode(_, writer = new wire_1.BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
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
        return exports.MsgEditValidatorResponse.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseMsgEditValidatorResponse();
        return message;
    }
};
exports.MsgDelegate = {
    $type: "cosmos.staking.v1beta1.MsgDelegate",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.delegator_address !== "") {
            writer.uint32(10).string(message.delegator_address);
        }
        if (message.validator_address !== "") {
            writer.uint32(18).string(message.validator_address);
        }
        if (message.amount !== undefined) {
            coin_1.Coin.encode(message.amount, writer.uint32(26).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
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
                    message.amount = coin_1.Coin.decode(reader, reader.uint32());
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
            amount: isSet(object.amount) ? coin_1.Coin.fromJSON(object.amount) : undefined
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
            obj.amount = coin_1.Coin.toJSON(message.amount);
        }
        return obj;
    },
    create(base) {
        return exports.MsgDelegate.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgDelegate();
        message.delegator_address = object.delegator_address ?? "";
        message.validator_address = object.validator_address ?? "";
        message.amount = object.amount !== undefined && object.amount !== null ? coin_1.Coin.fromPartial(object.amount) : undefined;
        return message;
    }
};
exports.MsgDelegateResponse = {
    $type: "cosmos.staking.v1beta1.MsgDelegateResponse",
    encode(_, writer = new wire_1.BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
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
        return exports.MsgDelegateResponse.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseMsgDelegateResponse();
        return message;
    }
};
exports.MsgBeginRedelegate = {
    $type: "cosmos.staking.v1beta1.MsgBeginRedelegate",
    encode(message, writer = new wire_1.BinaryWriter()) {
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
            coin_1.Coin.encode(message.amount, writer.uint32(34).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
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
                    message.amount = coin_1.Coin.decode(reader, reader.uint32());
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
            amount: isSet(object.amount) ? coin_1.Coin.fromJSON(object.amount) : undefined
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
            obj.amount = coin_1.Coin.toJSON(message.amount);
        }
        return obj;
    },
    create(base) {
        return exports.MsgBeginRedelegate.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgBeginRedelegate();
        message.delegator_address = object.delegator_address ?? "";
        message.validator_src_address = object.validator_src_address ?? "";
        message.validator_dst_address = object.validator_dst_address ?? "";
        message.amount = object.amount !== undefined && object.amount !== null ? coin_1.Coin.fromPartial(object.amount) : undefined;
        return message;
    }
};
exports.MsgBeginRedelegateResponse = {
    $type: "cosmos.staking.v1beta1.MsgBeginRedelegateResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.completion_time !== undefined) {
            timestamp_1.Timestamp.encode(toTimestamp(message.completion_time), writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgBeginRedelegateResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.completion_time = fromTimestamp(timestamp_1.Timestamp.decode(reader, reader.uint32()));
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
        return exports.MsgBeginRedelegateResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgBeginRedelegateResponse();
        message.completion_time = object.completion_time ?? undefined;
        return message;
    }
};
exports.MsgUndelegate = {
    $type: "cosmos.staking.v1beta1.MsgUndelegate",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.delegator_address !== "") {
            writer.uint32(10).string(message.delegator_address);
        }
        if (message.validator_address !== "") {
            writer.uint32(18).string(message.validator_address);
        }
        if (message.amount !== undefined) {
            coin_1.Coin.encode(message.amount, writer.uint32(26).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
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
                    message.amount = coin_1.Coin.decode(reader, reader.uint32());
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
            amount: isSet(object.amount) ? coin_1.Coin.fromJSON(object.amount) : undefined
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
            obj.amount = coin_1.Coin.toJSON(message.amount);
        }
        return obj;
    },
    create(base) {
        return exports.MsgUndelegate.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgUndelegate();
        message.delegator_address = object.delegator_address ?? "";
        message.validator_address = object.validator_address ?? "";
        message.amount = object.amount !== undefined && object.amount !== null ? coin_1.Coin.fromPartial(object.amount) : undefined;
        return message;
    }
};
exports.MsgUndelegateResponse = {
    $type: "cosmos.staking.v1beta1.MsgUndelegateResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.completion_time !== undefined) {
            timestamp_1.Timestamp.encode(toTimestamp(message.completion_time), writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgUndelegateResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.completion_time = fromTimestamp(timestamp_1.Timestamp.decode(reader, reader.uint32()));
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
        return exports.MsgUndelegateResponse.fromPartial(base ?? {});
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
        return fromTimestamp(timestamp_1.Timestamp.fromJSON(o));
    }
}
function isSet(value) {
    return value !== null && value !== undefined;
}
exports.registry = [
    ["/cosmos.staking.v1beta1.MsgCreateValidator", exports.MsgCreateValidator],
    ["/cosmos.staking.v1beta1.MsgCreateValidatorResponse", exports.MsgCreateValidatorResponse],
    ["/cosmos.staking.v1beta1.MsgEditValidator", exports.MsgEditValidator],
    ["/cosmos.staking.v1beta1.MsgEditValidatorResponse", exports.MsgEditValidatorResponse],
    ["/cosmos.staking.v1beta1.MsgDelegate", exports.MsgDelegate],
    ["/cosmos.staking.v1beta1.MsgDelegateResponse", exports.MsgDelegateResponse],
    ["/cosmos.staking.v1beta1.MsgBeginRedelegate", exports.MsgBeginRedelegate],
    ["/cosmos.staking.v1beta1.MsgBeginRedelegateResponse", exports.MsgBeginRedelegateResponse],
    ["/cosmos.staking.v1beta1.MsgUndelegate", exports.MsgUndelegate],
    ["/cosmos.staking.v1beta1.MsgUndelegateResponse", exports.MsgUndelegateResponse]
];
exports.aminoConverters = {
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
