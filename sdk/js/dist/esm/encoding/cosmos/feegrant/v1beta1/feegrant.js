import { BinaryReader, BinaryWriter } from "@bufbuild/protobuf/wire";
import { Any } from "../../../google/protobuf/any";
import { Duration } from "../../../google/protobuf/duration";
import { Timestamp } from "../../../google/protobuf/timestamp";
import { Coin } from "../../base/v1beta1/coin";
export const BasicAllowance = {
    $type: "cosmos.feegrant.v1beta1.BasicAllowance",
    encode(message, writer = new BinaryWriter()) {
        for (const v of message.spend_limit) {
            Coin.encode(v, writer.uint32(10).fork()).join();
        }
        if (message.expiration !== undefined) {
            Timestamp.encode(toTimestamp(message.expiration), writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseBasicAllowance();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.spend_limit.push(Coin.decode(reader, reader.uint32()));
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.expiration = fromTimestamp(Timestamp.decode(reader, reader.uint32()));
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
            spend_limit: globalThis.Array.isArray(object?.spend_limit) ? object.spend_limit.map((e) => Coin.fromJSON(e)) : [],
            expiration: isSet(object.expiration) ? fromJsonTimestamp(object.expiration) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.spend_limit?.length) {
            obj.spend_limit = message.spend_limit.map((e) => Coin.toJSON(e));
        }
        if (message.expiration !== undefined) {
            obj.expiration = message.expiration.toISOString();
        }
        return obj;
    },
    create(base) {
        return BasicAllowance.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseBasicAllowance();
        message.spend_limit = object.spend_limit?.map((e) => Coin.fromPartial(e)) || [];
        message.expiration = object.expiration ?? undefined;
        return message;
    }
};
export const PeriodicAllowance = {
    $type: "cosmos.feegrant.v1beta1.PeriodicAllowance",
    encode(message, writer = new BinaryWriter()) {
        if (message.basic !== undefined) {
            BasicAllowance.encode(message.basic, writer.uint32(10).fork()).join();
        }
        if (message.period !== undefined) {
            Duration.encode(message.period, writer.uint32(18).fork()).join();
        }
        for (const v of message.period_spend_limit) {
            Coin.encode(v, writer.uint32(26).fork()).join();
        }
        for (const v of message.period_can_spend) {
            Coin.encode(v, writer.uint32(34).fork()).join();
        }
        if (message.period_reset !== undefined) {
            Timestamp.encode(toTimestamp(message.period_reset), writer.uint32(42).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBasePeriodicAllowance();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.basic = BasicAllowance.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.period = Duration.decode(reader, reader.uint32());
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.period_spend_limit.push(Coin.decode(reader, reader.uint32()));
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.period_can_spend.push(Coin.decode(reader, reader.uint32()));
                    continue;
                case 5:
                    if (tag !== 42) {
                        break;
                    }
                    message.period_reset = fromTimestamp(Timestamp.decode(reader, reader.uint32()));
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
            basic: isSet(object.basic) ? BasicAllowance.fromJSON(object.basic) : undefined,
            period: isSet(object.period) ? Duration.fromJSON(object.period) : undefined,
            period_spend_limit: globalThis.Array.isArray(object?.period_spend_limit) ? object.period_spend_limit.map((e) => Coin.fromJSON(e)) : [],
            period_can_spend: globalThis.Array.isArray(object?.period_can_spend) ? object.period_can_spend.map((e) => Coin.fromJSON(e)) : [],
            period_reset: isSet(object.period_reset) ? fromJsonTimestamp(object.period_reset) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.basic !== undefined) {
            obj.basic = BasicAllowance.toJSON(message.basic);
        }
        if (message.period !== undefined) {
            obj.period = Duration.toJSON(message.period);
        }
        if (message.period_spend_limit?.length) {
            obj.period_spend_limit = message.period_spend_limit.map((e) => Coin.toJSON(e));
        }
        if (message.period_can_spend?.length) {
            obj.period_can_spend = message.period_can_spend.map((e) => Coin.toJSON(e));
        }
        if (message.period_reset !== undefined) {
            obj.period_reset = message.period_reset.toISOString();
        }
        return obj;
    },
    create(base) {
        return PeriodicAllowance.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBasePeriodicAllowance();
        message.basic = object.basic !== undefined && object.basic !== null ? BasicAllowance.fromPartial(object.basic) : undefined;
        message.period = object.period !== undefined && object.period !== null ? Duration.fromPartial(object.period) : undefined;
        message.period_spend_limit = object.period_spend_limit?.map((e) => Coin.fromPartial(e)) || [];
        message.period_can_spend = object.period_can_spend?.map((e) => Coin.fromPartial(e)) || [];
        message.period_reset = object.period_reset ?? undefined;
        return message;
    }
};
export const AllowedMsgAllowance = {
    $type: "cosmos.feegrant.v1beta1.AllowedMsgAllowance",
    encode(message, writer = new BinaryWriter()) {
        if (message.allowance !== undefined) {
            Any.encode(message.allowance, writer.uint32(10).fork()).join();
        }
        for (const v of message.allowed_messages) {
            writer.uint32(18).string(v);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseAllowedMsgAllowance();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.allowance = Any.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.allowed_messages.push(reader.string());
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
            allowance: isSet(object.allowance) ? Any.fromJSON(object.allowance) : undefined,
            allowed_messages: globalThis.Array.isArray(object?.allowed_messages) ? object.allowed_messages.map((e) => globalThis.String(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.allowance !== undefined) {
            obj.allowance = Any.toJSON(message.allowance);
        }
        if (message.allowed_messages?.length) {
            obj.allowed_messages = message.allowed_messages;
        }
        return obj;
    },
    create(base) {
        return AllowedMsgAllowance.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseAllowedMsgAllowance();
        message.allowance = object.allowance !== undefined && object.allowance !== null ? Any.fromPartial(object.allowance) : undefined;
        message.allowed_messages = object.allowed_messages?.map((e) => e) || [];
        return message;
    }
};
export const Grant = {
    $type: "cosmos.feegrant.v1beta1.Grant",
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
        const message = createBaseGrant();
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
        return Grant.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGrant();
        message.granter = object.granter ?? "";
        message.grantee = object.grantee ?? "";
        message.allowance = object.allowance !== undefined && object.allowance !== null ? Any.fromPartial(object.allowance) : undefined;
        return message;
    }
};
function createBaseBasicAllowance() {
    return { spend_limit: [], expiration: undefined };
}
function createBasePeriodicAllowance() {
    return { basic: undefined, period: undefined, period_spend_limit: [], period_can_spend: [], period_reset: undefined };
}
function createBaseAllowedMsgAllowance() {
    return { allowance: undefined, allowed_messages: [] };
}
function createBaseGrant() {
    return { granter: "", grantee: "", allowance: undefined };
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
    ["/cosmos.feegrant.v1beta1.BasicAllowance", BasicAllowance],
    ["/cosmos.feegrant.v1beta1.PeriodicAllowance", PeriodicAllowance],
    ["/cosmos.feegrant.v1beta1.AllowedMsgAllowance", AllowedMsgAllowance],
    ["/cosmos.feegrant.v1beta1.Grant", Grant]
];
export const aminoConverters = {
    "/cosmos.feegrant.v1beta1.BasicAllowance": {
        aminoType: "cosmos-sdk/BasicAllowance",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.feegrant.v1beta1.PeriodicAllowance": {
        aminoType: "cosmos-sdk/PeriodicAllowance",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.feegrant.v1beta1.AllowedMsgAllowance": {
        aminoType: "cosmos-sdk/AllowedMsgAllowance",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.feegrant.v1beta1.Grant": {
        aminoType: "cosmos-sdk/Grant",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
