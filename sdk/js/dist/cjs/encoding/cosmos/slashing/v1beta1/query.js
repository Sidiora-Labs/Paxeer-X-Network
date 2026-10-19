"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.QuerySigningInfosResponse = exports.QuerySigningInfosRequest = exports.QuerySigningInfoResponse = exports.QuerySigningInfoRequest = exports.QueryParamsResponse = exports.QueryParamsRequest = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
const pagination_1 = require("../../base/query/v1beta1/pagination");
const slashing_1 = require("./slashing");
exports.QueryParamsRequest = {
    $type: "cosmos.slashing.v1beta1.QueryParamsRequest",
    encode(_, writer = new wire_1.BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryParamsRequest();
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
        return exports.QueryParamsRequest.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseQueryParamsRequest();
        return message;
    }
};
exports.QueryParamsResponse = {
    $type: "cosmos.slashing.v1beta1.QueryParamsResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.params !== undefined) {
            slashing_1.Params.encode(message.params, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryParamsResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.params = slashing_1.Params.decode(reader, reader.uint32());
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
        return { params: isSet(object.params) ? slashing_1.Params.fromJSON(object.params) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.params !== undefined) {
            obj.params = slashing_1.Params.toJSON(message.params);
        }
        return obj;
    },
    create(base) {
        return exports.QueryParamsResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryParamsResponse();
        message.params = object.params !== undefined && object.params !== null ? slashing_1.Params.fromPartial(object.params) : undefined;
        return message;
    }
};
exports.QuerySigningInfoRequest = {
    $type: "cosmos.slashing.v1beta1.QuerySigningInfoRequest",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.cons_address !== "") {
            writer.uint32(10).string(message.cons_address);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQuerySigningInfoRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.cons_address = reader.string();
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
        return { cons_address: isSet(object.cons_address) ? globalThis.String(object.cons_address) : "" };
    },
    toJSON(message) {
        const obj = {};
        if (message.cons_address !== "") {
            obj.cons_address = message.cons_address;
        }
        return obj;
    },
    create(base) {
        return exports.QuerySigningInfoRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQuerySigningInfoRequest();
        message.cons_address = object.cons_address ?? "";
        return message;
    }
};
exports.QuerySigningInfoResponse = {
    $type: "cosmos.slashing.v1beta1.QuerySigningInfoResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.val_signing_info !== undefined) {
            slashing_1.ValidatorSigningInfo.encode(message.val_signing_info, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQuerySigningInfoResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.val_signing_info = slashing_1.ValidatorSigningInfo.decode(reader, reader.uint32());
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
            val_signing_info: isSet(object.val_signing_info) ? slashing_1.ValidatorSigningInfo.fromJSON(object.val_signing_info) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.val_signing_info !== undefined) {
            obj.val_signing_info = slashing_1.ValidatorSigningInfo.toJSON(message.val_signing_info);
        }
        return obj;
    },
    create(base) {
        return exports.QuerySigningInfoResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQuerySigningInfoResponse();
        message.val_signing_info =
            object.val_signing_info !== undefined && object.val_signing_info !== null ? slashing_1.ValidatorSigningInfo.fromPartial(object.val_signing_info) : undefined;
        return message;
    }
};
exports.QuerySigningInfosRequest = {
    $type: "cosmos.slashing.v1beta1.QuerySigningInfosRequest",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.pagination !== undefined) {
            pagination_1.PageRequest.encode(message.pagination, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQuerySigningInfosRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
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
        return { pagination: isSet(object.pagination) ? pagination_1.PageRequest.fromJSON(object.pagination) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.pagination !== undefined) {
            obj.pagination = pagination_1.PageRequest.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return exports.QuerySigningInfosRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQuerySigningInfosRequest();
        message.pagination = object.pagination !== undefined && object.pagination !== null ? pagination_1.PageRequest.fromPartial(object.pagination) : undefined;
        return message;
    }
};
exports.QuerySigningInfosResponse = {
    $type: "cosmos.slashing.v1beta1.QuerySigningInfosResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.info) {
            slashing_1.ValidatorSigningInfo.encode(v, writer.uint32(10).fork()).join();
        }
        if (message.pagination !== undefined) {
            pagination_1.PageResponse.encode(message.pagination, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQuerySigningInfosResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.info.push(slashing_1.ValidatorSigningInfo.decode(reader, reader.uint32()));
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
            info: globalThis.Array.isArray(object?.info) ? object.info.map((e) => slashing_1.ValidatorSigningInfo.fromJSON(e)) : [],
            pagination: isSet(object.pagination) ? pagination_1.PageResponse.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.info?.length) {
            obj.info = message.info.map((e) => slashing_1.ValidatorSigningInfo.toJSON(e));
        }
        if (message.pagination !== undefined) {
            obj.pagination = pagination_1.PageResponse.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return exports.QuerySigningInfosResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQuerySigningInfosResponse();
        message.info = object.info?.map((e) => slashing_1.ValidatorSigningInfo.fromPartial(e)) || [];
        message.pagination = object.pagination !== undefined && object.pagination !== null ? pagination_1.PageResponse.fromPartial(object.pagination) : undefined;
        return message;
    }
};
function createBaseQueryParamsRequest() {
    return {};
}
function createBaseQueryParamsResponse() {
    return { params: undefined };
}
function createBaseQuerySigningInfoRequest() {
    return { cons_address: "" };
}
function createBaseQuerySigningInfoResponse() {
    return { val_signing_info: undefined };
}
function createBaseQuerySigningInfosRequest() {
    return { pagination: undefined };
}
function createBaseQuerySigningInfosResponse() {
    return { info: [], pagination: undefined };
}
function isSet(value) {
    return value !== null && value !== undefined;
}
exports.registry = [
    ["/cosmos.slashing.v1beta1.QueryParamsRequest", exports.QueryParamsRequest],
    ["/cosmos.slashing.v1beta1.QueryParamsResponse", exports.QueryParamsResponse],
    ["/cosmos.slashing.v1beta1.QuerySigningInfoRequest", exports.QuerySigningInfoRequest],
    ["/cosmos.slashing.v1beta1.QuerySigningInfoResponse", exports.QuerySigningInfoResponse],
    ["/cosmos.slashing.v1beta1.QuerySigningInfosRequest", exports.QuerySigningInfosRequest],
    ["/cosmos.slashing.v1beta1.QuerySigningInfosResponse", exports.QuerySigningInfosResponse]
];
exports.aminoConverters = {
    "/cosmos.slashing.v1beta1.QueryParamsRequest": {
        aminoType: "cosmos-sdk/QueryParamsRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.slashing.v1beta1.QueryParamsResponse": {
        aminoType: "cosmos-sdk/QueryParamsResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.slashing.v1beta1.QuerySigningInfoRequest": {
        aminoType: "cosmos-sdk/QuerySigningInfoRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.slashing.v1beta1.QuerySigningInfoResponse": {
        aminoType: "cosmos-sdk/QuerySigningInfoResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.slashing.v1beta1.QuerySigningInfosRequest": {
        aminoType: "cosmos-sdk/QuerySigningInfosRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.slashing.v1beta1.QuerySigningInfosResponse": {
        aminoType: "cosmos-sdk/QuerySigningInfosResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
