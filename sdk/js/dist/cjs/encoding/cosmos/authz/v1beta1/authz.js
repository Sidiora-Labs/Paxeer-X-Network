"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.GrantAuthorization = exports.Grant = exports.GenericAuthorization = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
const any_1 = require("../../../google/protobuf/any");
const timestamp_1 = require("../../../google/protobuf/timestamp");
exports.GenericAuthorization = {
    $type: "cosmos.authz.v1beta1.GenericAuthorization",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.msg !== "") {
            writer.uint32(10).string(message.msg);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
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
        return exports.GenericAuthorization.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGenericAuthorization();
        message.msg = object.msg ?? "";
        return message;
    }
};
exports.Grant = {
    $type: "cosmos.authz.v1beta1.Grant",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.authorization !== undefined) {
            any_1.Any.encode(message.authorization, writer.uint32(10).fork()).join();
        }
        if (message.expiration !== undefined) {
            timestamp_1.Timestamp.encode(toTimestamp(message.expiration), writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseGrant();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.authorization = any_1.Any.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.expiration = fromTimestamp(timestamp_1.Timestamp.decode(reader, reader.uint32()));
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
            authorization: isSet(object.authorization) ? any_1.Any.fromJSON(object.authorization) : undefined,
            expiration: isSet(object.expiration) ? fromJsonTimestamp(object.expiration) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.authorization !== undefined) {
            obj.authorization = any_1.Any.toJSON(message.authorization);
        }
        if (message.expiration !== undefined) {
            obj.expiration = message.expiration.toISOString();
        }
        return obj;
    },
    create(base) {
        return exports.Grant.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGrant();
        message.authorization = object.authorization !== undefined && object.authorization !== null ? any_1.Any.fromPartial(object.authorization) : undefined;
        message.expiration = object.expiration ?? undefined;
        return message;
    }
};
exports.GrantAuthorization = {
    $type: "cosmos.authz.v1beta1.GrantAuthorization",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.granter !== "") {
            writer.uint32(10).string(message.granter);
        }
        if (message.grantee !== "") {
            writer.uint32(18).string(message.grantee);
        }
        if (message.authorization !== undefined) {
            any_1.Any.encode(message.authorization, writer.uint32(26).fork()).join();
        }
        if (message.expiration !== undefined) {
            timestamp_1.Timestamp.encode(toTimestamp(message.expiration), writer.uint32(34).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
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
                    message.authorization = any_1.Any.decode(reader, reader.uint32());
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.expiration = fromTimestamp(timestamp_1.Timestamp.decode(reader, reader.uint32()));
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
            authorization: isSet(object.authorization) ? any_1.Any.fromJSON(object.authorization) : undefined,
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
            obj.authorization = any_1.Any.toJSON(message.authorization);
        }
        if (message.expiration !== undefined) {
            obj.expiration = message.expiration.toISOString();
        }
        return obj;
    },
    create(base) {
        return exports.GrantAuthorization.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGrantAuthorization();
        message.granter = object.granter ?? "";
        message.grantee = object.grantee ?? "";
        message.authorization = object.authorization !== undefined && object.authorization !== null ? any_1.Any.fromPartial(object.authorization) : undefined;
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
        return fromTimestamp(timestamp_1.Timestamp.fromJSON(o));
    }
}
function isSet(value) {
    return value !== null && value !== undefined;
}
exports.registry = [
    ["/cosmos.authz.v1beta1.GenericAuthorization", exports.GenericAuthorization],
    ["/cosmos.authz.v1beta1.Grant", exports.Grant],
    ["/cosmos.authz.v1beta1.GrantAuthorization", exports.GrantAuthorization]
];
exports.aminoConverters = {
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
