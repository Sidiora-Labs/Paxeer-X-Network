"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.QueryGranteeGrantsResponse = exports.QueryGranteeGrantsRequest = exports.QueryGranterGrantsResponse = exports.QueryGranterGrantsRequest = exports.QueryGrantsResponse = exports.QueryGrantsRequest = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
const pagination_1 = require("../../base/query/v1beta1/pagination");
const authz_1 = require("./authz");
exports.QueryGrantsRequest = {
    $type: "cosmos.authz.v1beta1.QueryGrantsRequest",
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
        if (message.pagination !== undefined) {
            pagination_1.PageRequest.encode(message.pagination, writer.uint32(34).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryGrantsRequest();
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
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.pagination = pagination_1.PageRequest.decode(reader, reader.uint32());
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
            msg_type_url: isSet(object.msg_type_url) ? globalThis.String(object.msg_type_url) : "",
            pagination: isSet(object.pagination) ? pagination_1.PageRequest.fromJSON(object.pagination) : undefined
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
        if (message.pagination !== undefined) {
            obj.pagination = pagination_1.PageRequest.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return exports.QueryGrantsRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryGrantsRequest();
        message.granter = object.granter ?? "";
        message.grantee = object.grantee ?? "";
        message.msg_type_url = object.msg_type_url ?? "";
        message.pagination = object.pagination !== undefined && object.pagination !== null ? pagination_1.PageRequest.fromPartial(object.pagination) : undefined;
        return message;
    }
};
exports.QueryGrantsResponse = {
    $type: "cosmos.authz.v1beta1.QueryGrantsResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.grants) {
            authz_1.Grant.encode(v, writer.uint32(10).fork()).join();
        }
        if (message.pagination !== undefined) {
            pagination_1.PageResponse.encode(message.pagination, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryGrantsResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.grants.push(authz_1.Grant.decode(reader, reader.uint32()));
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.pagination = pagination_1.PageResponse.decode(reader, reader.uint32());
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
            grants: globalThis.Array.isArray(object?.grants) ? object.grants.map((e) => authz_1.Grant.fromJSON(e)) : [],
            pagination: isSet(object.pagination) ? pagination_1.PageResponse.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.grants?.length) {
            obj.grants = message.grants.map((e) => authz_1.Grant.toJSON(e));
        }
        if (message.pagination !== undefined) {
            obj.pagination = pagination_1.PageResponse.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return exports.QueryGrantsResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryGrantsResponse();
        message.grants = object.grants?.map((e) => authz_1.Grant.fromPartial(e)) || [];
        message.pagination = object.pagination !== undefined && object.pagination !== null ? pagination_1.PageResponse.fromPartial(object.pagination) : undefined;
        return message;
    }
};
exports.QueryGranterGrantsRequest = {
    $type: "cosmos.authz.v1beta1.QueryGranterGrantsRequest",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.granter !== "") {
            writer.uint32(10).string(message.granter);
        }
        if (message.pagination !== undefined) {
            pagination_1.PageRequest.encode(message.pagination, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryGranterGrantsRequest();
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
                    message.pagination = pagination_1.PageRequest.decode(reader, reader.uint32());
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
            pagination: isSet(object.pagination) ? pagination_1.PageRequest.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.granter !== "") {
            obj.granter = message.granter;
        }
        if (message.pagination !== undefined) {
            obj.pagination = pagination_1.PageRequest.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return exports.QueryGranterGrantsRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryGranterGrantsRequest();
        message.granter = object.granter ?? "";
        message.pagination = object.pagination !== undefined && object.pagination !== null ? pagination_1.PageRequest.fromPartial(object.pagination) : undefined;
        return message;
    }
};
exports.QueryGranterGrantsResponse = {
    $type: "cosmos.authz.v1beta1.QueryGranterGrantsResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.grants) {
            authz_1.GrantAuthorization.encode(v, writer.uint32(10).fork()).join();
        }
        if (message.pagination !== undefined) {
            pagination_1.PageResponse.encode(message.pagination, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryGranterGrantsResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.grants.push(authz_1.GrantAuthorization.decode(reader, reader.uint32()));
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.pagination = pagination_1.PageResponse.decode(reader, reader.uint32());
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
            grants: globalThis.Array.isArray(object?.grants) ? object.grants.map((e) => authz_1.GrantAuthorization.fromJSON(e)) : [],
            pagination: isSet(object.pagination) ? pagination_1.PageResponse.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.grants?.length) {
            obj.grants = message.grants.map((e) => authz_1.GrantAuthorization.toJSON(e));
        }
        if (message.pagination !== undefined) {
            obj.pagination = pagination_1.PageResponse.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return exports.QueryGranterGrantsResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryGranterGrantsResponse();
        message.grants = object.grants?.map((e) => authz_1.GrantAuthorization.fromPartial(e)) || [];
        message.pagination = object.pagination !== undefined && object.pagination !== null ? pagination_1.PageResponse.fromPartial(object.pagination) : undefined;
        return message;
    }
};
exports.QueryGranteeGrantsRequest = {
    $type: "cosmos.authz.v1beta1.QueryGranteeGrantsRequest",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.grantee !== "") {
            writer.uint32(10).string(message.grantee);
        }
        if (message.pagination !== undefined) {
            pagination_1.PageRequest.encode(message.pagination, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryGranteeGrantsRequest();
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
                    message.pagination = pagination_1.PageRequest.decode(reader, reader.uint32());
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
            pagination: isSet(object.pagination) ? pagination_1.PageRequest.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.grantee !== "") {
            obj.grantee = message.grantee;
        }
        if (message.pagination !== undefined) {
            obj.pagination = pagination_1.PageRequest.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return exports.QueryGranteeGrantsRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryGranteeGrantsRequest();
        message.grantee = object.grantee ?? "";
        message.pagination = object.pagination !== undefined && object.pagination !== null ? pagination_1.PageRequest.fromPartial(object.pagination) : undefined;
        return message;
    }
};
exports.QueryGranteeGrantsResponse = {
    $type: "cosmos.authz.v1beta1.QueryGranteeGrantsResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.grants) {
            authz_1.GrantAuthorization.encode(v, writer.uint32(10).fork()).join();
        }
        if (message.pagination !== undefined) {
            pagination_1.PageResponse.encode(message.pagination, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryGranteeGrantsResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.grants.push(authz_1.GrantAuthorization.decode(reader, reader.uint32()));
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.pagination = pagination_1.PageResponse.decode(reader, reader.uint32());
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
            grants: globalThis.Array.isArray(object?.grants) ? object.grants.map((e) => authz_1.GrantAuthorization.fromJSON(e)) : [],
            pagination: isSet(object.pagination) ? pagination_1.PageResponse.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.grants?.length) {
            obj.grants = message.grants.map((e) => authz_1.GrantAuthorization.toJSON(e));
        }
        if (message.pagination !== undefined) {
            obj.pagination = pagination_1.PageResponse.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return exports.QueryGranteeGrantsResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryGranteeGrantsResponse();
        message.grants = object.grants?.map((e) => authz_1.GrantAuthorization.fromPartial(e)) || [];
        message.pagination = object.pagination !== undefined && object.pagination !== null ? pagination_1.PageResponse.fromPartial(object.pagination) : undefined;
        return message;
    }
};
function createBaseQueryGrantsRequest() {
    return { granter: "", grantee: "", msg_type_url: "", pagination: undefined };
}
function createBaseQueryGrantsResponse() {
    return { grants: [], pagination: undefined };
}
function createBaseQueryGranterGrantsRequest() {
    return { granter: "", pagination: undefined };
}
function createBaseQueryGranterGrantsResponse() {
    return { grants: [], pagination: undefined };
}
function createBaseQueryGranteeGrantsRequest() {
    return { grantee: "", pagination: undefined };
}
function createBaseQueryGranteeGrantsResponse() {
    return { grants: [], pagination: undefined };
}
function isSet(value) {
    return value !== null && value !== undefined;
}
exports.registry = [
    ["/cosmos.authz.v1beta1.QueryGrantsRequest", exports.QueryGrantsRequest],
    ["/cosmos.authz.v1beta1.QueryGrantsResponse", exports.QueryGrantsResponse],
    ["/cosmos.authz.v1beta1.QueryGranterGrantsRequest", exports.QueryGranterGrantsRequest],
    ["/cosmos.authz.v1beta1.QueryGranterGrantsResponse", exports.QueryGranterGrantsResponse],
    ["/cosmos.authz.v1beta1.QueryGranteeGrantsRequest", exports.QueryGranteeGrantsRequest],
    ["/cosmos.authz.v1beta1.QueryGranteeGrantsResponse", exports.QueryGranteeGrantsResponse]
];
exports.aminoConverters = {
    "/cosmos.authz.v1beta1.QueryGrantsRequest": {
        aminoType: "cosmos-sdk/QueryGrantsRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.authz.v1beta1.QueryGrantsResponse": {
        aminoType: "cosmos-sdk/QueryGrantsResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.authz.v1beta1.QueryGranterGrantsRequest": {
        aminoType: "cosmos-sdk/QueryGranterGrantsRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.authz.v1beta1.QueryGranterGrantsResponse": {
        aminoType: "cosmos-sdk/QueryGranterGrantsResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.authz.v1beta1.QueryGranteeGrantsRequest": {
        aminoType: "cosmos-sdk/QueryGranteeGrantsRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.authz.v1beta1.QueryGranteeGrantsResponse": {
        aminoType: "cosmos-sdk/QueryGranteeGrantsResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
