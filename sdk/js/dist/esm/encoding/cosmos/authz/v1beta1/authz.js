import { BinaryReader, BinaryWriter } from "@bufbuild/protobuf/wire";
import { Any } from "../../../google/protobuf/any";
import { Timestamp } from "../../../google/protobuf/timestamp";
export const GenericAuthorization = {
    $type: "cosmos.authz.v1beta1.GenericAuthorization",
    encode(message, writer = new BinaryWriter()) {
        if (message.msg !== "") {
            writer.uint32(10).string(message.msg);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseGenericAuthorization();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.msg = reader.string();
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
        return { msg: isSet(object.msg) ? globalThis.String(object.msg) : "" };
    },
    toJSON(message) {
        const obj = {};
        if (message.msg !== "") {
            obj.msg = message.msg;
        }
        return obj;
    },
    create(base) {
        return GenericAuthorization.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGenericAuthorization();
        message.msg = object.msg ?? "";
        return message;
    }
};
export const Grant = {
    $type: "cosmos.authz.v1beta1.Grant",
    encode(message, writer = new BinaryWriter()) {
        if (message.authorization !== undefined) {
            Any.encode(message.authorization, writer.uint32(10).fork()).join();
        }
        if (message.expiration !== undefined) {
            Timestamp.encode(toTimestamp(message.expiration), writer.uint32(18).fork()).join();
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
                    message.authorization = Any.decode(reader, reader.uint32());
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
            authorization: isSet(object.authorization) ? Any.fromJSON(object.authorization) : undefined,
            expiration: isSet(object.expiration) ? fromJsonTimestamp(object.expiration) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.authorization !== undefined) {
            obj.authorization = Any.toJSON(message.authorization);
        }
        if (message.expiration !== undefined) {
            obj.expiration = message.expiration.toISOString();
        }
        return obj;
    },
    create(base) {
        return Grant.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGrant();
        message.authorization = object.authorization !== undefined && object.authorization !== null ? Any.fromPartial(object.authorization) : undefined;
        message.expiration = object.expiration ?? undefined;
        return message;
    }
};
export const GrantAuthorization = {
    $type: "cosmos.authz.v1beta1.GrantAuthorization",
    encode(message, writer = new BinaryWriter()) {
        if (message.granter !== "") {
            writer.uint32(10).string(message.granter);
        }
        if (message.grantee !== "") {
            writer.uint32(18).string(message.grantee);
        }
        if (message.authorization !== undefined) {
            Any.encode(message.authorization, writer.uint32(26).fork()).join();
        }
        if (message.expiration !== undefined) {
            Timestamp.encode(toTimestamp(message.expiration), writer.uint32(34).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseGrantAuthorization();
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
                    message.authorization = Any.decode(reader, reader.uint32());
                    continue;
                case 4:
                    if (tag !== 34) {
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
            granter: isSet(object.granter) ? globalThis.String(object.granter) : "",
            grantee: isSet(object.grantee) ? globalThis.String(object.grantee) : "",
            authorization: isSet(object.authorization) ? Any.fromJSON(object.authorization) : undefined,
            expiration: isSet(object.expiration) ? fromJsonTimestamp(object.expiration) : undefined
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
        if (message.authorization !== undefined) {
            obj.authorization = Any.toJSON(message.authorization);
        }
        if (message.expiration !== undefined) {
            obj.expiration = message.expiration.toISOString();
        }
        return obj;
    },
    create(base) {
        return GrantAuthorization.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGrantAuthorization();
        message.granter = object.granter ?? "";
        message.grantee = object.grantee ?? "";
        message.authorization = object.authorization !== undefined && object.authorization !== null ? Any.fromPartial(object.authorization) : undefined;
        message.expiration = object.expiration ?? undefined;
        return message;
    }
};
function createBaseGenericAuthorization() {
    return { msg: "" };
}
function createBaseGrant() {
    return { authorization: undefined, expiration: undefined };
}
function createBaseGrantAuthorization() {
    return { granter: "", grantee: "", authorization: undefined, expiration: undefined };
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
    ["/cosmos.authz.v1beta1.GenericAuthorization", GenericAuthorization],
    ["/cosmos.authz.v1beta1.Grant", Grant],
    ["/cosmos.authz.v1beta1.GrantAuthorization", GrantAuthorization]
];
export const aminoConverters = {
    "/cosmos.authz.v1beta1.GenericAuthorization": {
        aminoType: "cosmos-sdk/GenericAuthorization",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.authz.v1beta1.Grant": {
        aminoType: "cosmos-sdk/Grant",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.authz.v1beta1.GrantAuthorization": {
        aminoType: "cosmos-sdk/GrantAuthorization",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
