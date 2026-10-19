"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.MsgRevokeResponse = exports.MsgRevoke = exports.MsgGrantResponse = exports.MsgExec = exports.MsgExecResponse = exports.MsgGrant = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
const any_1 = require("../../../google/protobuf/any");
const authz_1 = require("./authz");
exports.MsgGrant = {
    $type: "cosmos.authz.v1beta1.MsgGrant",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.granter !== "") {
            writer.uint32(10).string(message.granter);
        }
        if (message.grantee !== "") {
            writer.uint32(18).string(message.grantee);
        }
        if (message.grant !== undefined) {
            authz_1.Grant.encode(message.grant, writer.uint32(26).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgGrant();
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
                    message.grant = authz_1.Grant.decode(reader, reader.uint32());
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
            grant: isSet(object.grant) ? authz_1.Grant.fromJSON(object.grant) : undefined
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
        if (message.grant !== undefined) {
            obj.grant = authz_1.Grant.toJSON(message.grant);
        }
        return obj;
    },
    create(base) {
        return exports.MsgGrant.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgGrant();
        message.granter = object.granter ?? "";
        message.grantee = object.grantee ?? "";
        message.grant = object.grant !== undefined && object.grant !== null ? authz_1.Grant.fromPartial(object.grant) : undefined;
        return message;
    }
};
exports.MsgExecResponse = {
    $type: "cosmos.authz.v1beta1.MsgExecResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.results) {
            writer.uint32(10).bytes(v);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgExecResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.results.push(reader.bytes());
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
            results: globalThis.Array.isArray(object?.results) ? object.results.map((e) => bytesFromBase64(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.results?.length) {
            obj.results = message.results.map((e) => base64FromBytes(e));
        }
        return obj;
    },
    create(base) {
        return exports.MsgExecResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgExecResponse();
        message.results = object.results?.map((e) => e) || [];
        return message;
    }
};
exports.MsgExec = {
    $type: "cosmos.authz.v1beta1.MsgExec",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.grantee !== "") {
            writer.uint32(10).string(message.grantee);
        }
        for (const v of message.msgs) {
            any_1.Any.encode(v, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgExec();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.grantee = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.msgs.push(any_1.Any.decode(reader, reader.uint32()));
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
            grantee: isSet(object.grantee) ? globalThis.String(object.grantee) : "",
            msgs: globalThis.Array.isArray(object?.msgs) ? object.msgs.map((e) => any_1.Any.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.grantee !== "") {
            obj.grantee = message.grantee;
        }
        if (message.msgs?.length) {
            obj.msgs = message.msgs.map((e) => any_1.Any.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.MsgExec.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgExec();
        message.grantee = object.grantee ?? "";
        message.msgs = object.msgs?.map((e) => any_1.Any.fromPartial(e)) || [];
        return message;
    }
};
exports.MsgGrantResponse = {
    $type: "cosmos.authz.v1beta1.MsgGrantResponse",
    encode(_, writer = new wire_1.BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgGrantResponse();
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
        return exports.MsgGrantResponse.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseMsgGrantResponse();
        return message;
    }
};
exports.MsgRevoke = {
    $type: "cosmos.authz.v1beta1.MsgRevoke",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.granter !== "") {
            writer.uint32(10).string(message.granter);
        }
        if (message.grantee !== "") {
            writer.uint32(18).string(message.grantee);
        }
        if (message.msg_type_url !== "") {
            writer.uint32(26).string(message.msg_type_url);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgRevoke();
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
                    message.msg_type_url = reader.string();
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
            msg_type_url: isSet(object.msg_type_url) ? globalThis.String(object.msg_type_url) : ""
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
        if (message.msg_type_url !== "") {
            obj.msg_type_url = message.msg_type_url;
        }
        return obj;
    },
    create(base) {
        return exports.MsgRevoke.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgRevoke();
        message.granter = object.granter ?? "";
        message.grantee = object.grantee ?? "";
        message.msg_type_url = object.msg_type_url ?? "";
        return message;
    }
};
exports.MsgRevokeResponse = {
    $type: "cosmos.authz.v1beta1.MsgRevokeResponse",
    encode(_, writer = new wire_1.BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgRevokeResponse();
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
        return exports.MsgRevokeResponse.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseMsgRevokeResponse();
        return message;
    }
};
function createBaseMsgGrant() {
    return { granter: "", grantee: "", grant: undefined };
}
function createBaseMsgExecResponse() {
    return { results: [] };
}
function createBaseMsgExec() {
    return { grantee: "", msgs: [] };
}
function createBaseMsgGrantResponse() {
    return {};
}
function createBaseMsgRevoke() {
    return { granter: "", grantee: "", msg_type_url: "" };
}
function createBaseMsgRevokeResponse() {
    return {};
}
function bytesFromBase64(b64) {
    if (globalThis.Buffer) {
        return Uint8Array.from(globalThis.Buffer.from(b64, "base64"));
    }
    else {
        const bin = globalThis.atob(b64);
        const arr = new Uint8Array(bin.length);
        for (let i = 0; i < bin.length; ++i) {
            arr[i] = bin.charCodeAt(i);
        }
        return arr;
    }
}
function base64FromBytes(arr) {
    if (globalThis.Buffer) {
        return globalThis.Buffer.from(arr).toString("base64");
    }
    else {
        const bin = [];
        arr.forEach((byte) => {
            bin.push(globalThis.String.fromCharCode(byte));
        });
        return globalThis.btoa(bin.join(""));
    }
}
function isSet(value) {
    return value !== null && value !== undefined;
}
exports.registry = [
    ["/cosmos.authz.v1beta1.MsgGrant", exports.MsgGrant],
    ["/cosmos.authz.v1beta1.MsgExecResponse", exports.MsgExecResponse],
    ["/cosmos.authz.v1beta1.MsgExec", exports.MsgExec],
    ["/cosmos.authz.v1beta1.MsgGrantResponse", exports.MsgGrantResponse],
    ["/cosmos.authz.v1beta1.MsgRevoke", exports.MsgRevoke],
    ["/cosmos.authz.v1beta1.MsgRevokeResponse", exports.MsgRevokeResponse]
];
exports.aminoConverters = {
    "/cosmos.authz.v1beta1.MsgGrant": {
        aminoType: "cosmos-sdk/MsgGrant",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.authz.v1beta1.MsgExecResponse": {
        aminoType: "cosmos-sdk/MsgExecResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.authz.v1beta1.MsgExec": {
        aminoType: "cosmos-sdk/MsgExec",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.authz.v1beta1.MsgGrantResponse": {
        aminoType: "cosmos-sdk/MsgGrantResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.authz.v1beta1.MsgRevoke": {
        aminoType: "cosmos-sdk/MsgRevoke",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.authz.v1beta1.MsgRevokeResponse": {
        aminoType: "cosmos-sdk/MsgRevokeResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
