import { BinaryReader, BinaryWriter } from "@bufbuild/protobuf/wire";
export const EventGrant = {
    $type: "cosmos.authz.v1beta1.EventGrant",
    encode(message, writer = new BinaryWriter()) {
        if (message.msg_type_url !== "") {
            writer.uint32(18).string(message.msg_type_url);
        }
        if (message.granter !== "") {
            writer.uint32(26).string(message.granter);
        }
        if (message.grantee !== "") {
            writer.uint32(34).string(message.grantee);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseEventGrant();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.msg_type_url = reader.string();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.granter = reader.string();
                    continue;
                case 4:
                    if (tag !== 34) {
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
            msg_type_url: isSet(object.msg_type_url) ? globalThis.String(object.msg_type_url) : "",
            granter: isSet(object.granter) ? globalThis.String(object.granter) : "",
            grantee: isSet(object.grantee) ? globalThis.String(object.grantee) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.msg_type_url !== "") {
            obj.msg_type_url = message.msg_type_url;
        }
        if (message.granter !== "") {
            obj.granter = message.granter;
        }
        if (message.grantee !== "") {
            obj.grantee = message.grantee;
        }
        return obj;
    },
    create(base) {
        return EventGrant.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseEventGrant();
        message.msg_type_url = object.msg_type_url ?? "";
        message.granter = object.granter ?? "";
        message.grantee = object.grantee ?? "";
        return message;
    }
};
export const EventRevoke = {
    $type: "cosmos.authz.v1beta1.EventRevoke",
    encode(message, writer = new BinaryWriter()) {
        if (message.msg_type_url !== "") {
            writer.uint32(18).string(message.msg_type_url);
        }
        if (message.granter !== "") {
            writer.uint32(26).string(message.granter);
        }
        if (message.grantee !== "") {
            writer.uint32(34).string(message.grantee);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseEventRevoke();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.msg_type_url = reader.string();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.granter = reader.string();
                    continue;
                case 4:
                    if (tag !== 34) {
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
            msg_type_url: isSet(object.msg_type_url) ? globalThis.String(object.msg_type_url) : "",
            granter: isSet(object.granter) ? globalThis.String(object.granter) : "",
            grantee: isSet(object.grantee) ? globalThis.String(object.grantee) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.msg_type_url !== "") {
            obj.msg_type_url = message.msg_type_url;
        }
        if (message.granter !== "") {
            obj.granter = message.granter;
        }
        if (message.grantee !== "") {
            obj.grantee = message.grantee;
        }
        return obj;
    },
    create(base) {
        return EventRevoke.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseEventRevoke();
        message.msg_type_url = object.msg_type_url ?? "";
        message.granter = object.granter ?? "";
        message.grantee = object.grantee ?? "";
        return message;
    }
};
function createBaseEventGrant() {
    return { msg_type_url: "", granter: "", grantee: "" };
}
function createBaseEventRevoke() {
    return { msg_type_url: "", granter: "", grantee: "" };
}
function isSet(value) {
    return value !== null && value !== undefined;
}
export const registry = [
    ["/cosmos.authz.v1beta1.EventGrant", EventGrant],
    ["/cosmos.authz.v1beta1.EventRevoke", EventRevoke]
];
export const aminoConverters = {
    "/cosmos.authz.v1beta1.EventGrant": {
        aminoType: "cosmos-sdk/EventGrant",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.authz.v1beta1.EventRevoke": {
        aminoType: "cosmos-sdk/EventRevoke",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
