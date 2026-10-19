"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.QueryAllowancesByGranterResponse = exports.QueryAllowancesByGranterRequest = exports.QueryAllowancesResponse = exports.QueryAllowancesRequest = exports.QueryAllowanceResponse = exports.QueryAllowanceRequest = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
const pagination_1 = require("../../base/query/v1beta1/pagination");
const feegrant_1 = require("./feegrant");
exports.QueryAllowanceRequest = {
    $type: "cosmos.feegrant.v1beta1.QueryAllowanceRequest",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.granter !== "") {
            writer.uint32(10).string(message.granter);
        }
        if (message.grantee !== "") {
            writer.uint32(18).string(message.grantee);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryAllowanceRequest();
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
            grantee: isSet(object.grantee) ? globalThis.String(object.grantee) : ""
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
        return obj;
    },
    create(base) {
        return exports.QueryAllowanceRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryAllowanceRequest();
        message.granter = object.granter ?? "";
        message.grantee = object.grantee ?? "";
        return message;
    }
};
exports.QueryAllowanceResponse = {
    $type: "cosmos.feegrant.v1beta1.QueryAllowanceResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.allowance !== undefined) {
            feegrant_1.Grant.encode(message.allowance, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryAllowanceResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.allowance = feegrant_1.Grant.decode(reader, reader.uint32());
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
        return { allowance: isSet(object.allowance) ? feegrant_1.Grant.fromJSON(object.allowance) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.allowance !== undefined) {
            obj.allowance = feegrant_1.Grant.toJSON(message.allowance);
        }
        return obj;
    },
    create(base) {
        return exports.QueryAllowanceResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryAllowanceResponse();
        message.allowance = object.allowance !== undefined && object.allowance !== null ? feegrant_1.Grant.fromPartial(object.allowance) : undefined;
        return message;
    }
};
exports.QueryAllowancesRequest = {
    $type: "cosmos.feegrant.v1beta1.QueryAllowancesRequest",
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
        const message = createBaseQueryAllowancesRequest();
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
        return exports.QueryAllowancesRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryAllowancesRequest();
        message.grantee = object.grantee ?? "";
        message.pagination = object.pagination !== undefined && object.pagination !== null ? pagination_1.PageRequest.fromPartial(object.pagination) : undefined;
        return message;
    }
};
exports.QueryAllowancesResponse = {
    $type: "cosmos.feegrant.v1beta1.QueryAllowancesResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.allowances) {
            feegrant_1.Grant.encode(v, writer.uint32(10).fork()).join();
        }
        if (message.pagination !== undefined) {
            pagination_1.PageResponse.encode(message.pagination, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryAllowancesResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.allowances.push(feegrant_1.Grant.decode(reader, reader.uint32()));
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
            allowances: globalThis.Array.isArray(object?.allowances) ? object.allowances.map((e) => feegrant_1.Grant.fromJSON(e)) : [],
            pagination: isSet(object.pagination) ? pagination_1.PageResponse.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.allowances?.length) {
            obj.allowances = message.allowances.map((e) => feegrant_1.Grant.toJSON(e));
        }
        if (message.pagination !== undefined) {
            obj.pagination = pagination_1.PageResponse.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return exports.QueryAllowancesResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryAllowancesResponse();
        message.allowances = object.allowances?.map((e) => feegrant_1.Grant.fromPartial(e)) || [];
        message.pagination = object.pagination !== undefined && object.pagination !== null ? pagination_1.PageResponse.fromPartial(object.pagination) : undefined;
        return message;
    }
};
exports.QueryAllowancesByGranterRequest = {
    $type: "cosmos.feegrant.v1beta1.QueryAllowancesByGranterRequest",
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
        const message = createBaseQueryAllowancesByGranterRequest();
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
        return exports.QueryAllowancesByGranterRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryAllowancesByGranterRequest();
        message.granter = object.granter ?? "";
        message.pagination = object.pagination !== undefined && object.pagination !== null ? pagination_1.PageRequest.fromPartial(object.pagination) : undefined;
        return message;
    }
};
exports.QueryAllowancesByGranterResponse = {
    $type: "cosmos.feegrant.v1beta1.QueryAllowancesByGranterResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.allowances) {
            feegrant_1.Grant.encode(v, writer.uint32(10).fork()).join();
        }
        if (message.pagination !== undefined) {
            pagination_1.PageResponse.encode(message.pagination, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryAllowancesByGranterResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.allowances.push(feegrant_1.Grant.decode(reader, reader.uint32()));
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
            allowances: globalThis.Array.isArray(object?.allowances) ? object.allowances.map((e) => feegrant_1.Grant.fromJSON(e)) : [],
            pagination: isSet(object.pagination) ? pagination_1.PageResponse.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.allowances?.length) {
            obj.allowances = message.allowances.map((e) => feegrant_1.Grant.toJSON(e));
        }
        if (message.pagination !== undefined) {
            obj.pagination = pagination_1.PageResponse.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return exports.QueryAllowancesByGranterResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryAllowancesByGranterResponse();
        message.allowances = object.allowances?.map((e) => feegrant_1.Grant.fromPartial(e)) || [];
        message.pagination = object.pagination !== undefined && object.pagination !== null ? pagination_1.PageResponse.fromPartial(object.pagination) : undefined;
        return message;
    }
};
function createBaseQueryAllowanceRequest() {
    return { granter: "", grantee: "" };
}
function createBaseQueryAllowanceResponse() {
    return { allowance: undefined };
}
function createBaseQueryAllowancesRequest() {
    return { grantee: "", pagination: undefined };
}
function createBaseQueryAllowancesResponse() {
    return { allowances: [], pagination: undefined };
}
function createBaseQueryAllowancesByGranterRequest() {
    return { granter: "", pagination: undefined };
}
function createBaseQueryAllowancesByGranterResponse() {
    return { allowances: [], pagination: undefined };
}
function isSet(value) {
    return value !== null && value !== undefined;
}
exports.registry = [
    ["/cosmos.feegrant.v1beta1.QueryAllowanceRequest", exports.QueryAllowanceRequest],
    ["/cosmos.feegrant.v1beta1.QueryAllowanceResponse", exports.QueryAllowanceResponse],
    ["/cosmos.feegrant.v1beta1.QueryAllowancesRequest", exports.QueryAllowancesRequest],
    ["/cosmos.feegrant.v1beta1.QueryAllowancesResponse", exports.QueryAllowancesResponse],
    ["/cosmos.feegrant.v1beta1.QueryAllowancesByGranterRequest", exports.QueryAllowancesByGranterRequest],
    ["/cosmos.feegrant.v1beta1.QueryAllowancesByGranterResponse", exports.QueryAllowancesByGranterResponse]
];
exports.aminoConverters = {
    "/cosmos.feegrant.v1beta1.QueryAllowanceRequest": {
        aminoType: "cosmos-sdk/QueryAllowanceRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.feegrant.v1beta1.QueryAllowanceResponse": {
        aminoType: "cosmos-sdk/QueryAllowanceResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.feegrant.v1beta1.QueryAllowancesRequest": {
        aminoType: "cosmos-sdk/QueryAllowancesRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.feegrant.v1beta1.QueryAllowancesResponse": {
        aminoType: "cosmos-sdk/QueryAllowancesResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.feegrant.v1beta1.QueryAllowancesByGranterRequest": {
        aminoType: "cosmos-sdk/QueryAllowancesByGranterRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.feegrant.v1beta1.QueryAllowancesByGranterResponse": {
        aminoType: "cosmos-sdk/QueryAllowancesByGranterResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
