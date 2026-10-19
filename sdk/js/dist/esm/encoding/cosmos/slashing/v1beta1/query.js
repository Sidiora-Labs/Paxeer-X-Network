import { BinaryReader, BinaryWriter } from "@bufbuild/protobuf/wire";
import { PageRequest, PageResponse } from "../../base/query/v1beta1/pagination";
import { Params, ValidatorSigningInfo } from "./slashing";
export const QueryParamsRequest = {
    $type: "cosmos.slashing.v1beta1.QueryParamsRequest",
    encode(_, writer = new BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
        return QueryParamsRequest.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseQueryParamsRequest();
        return message;
    }
};
export const QueryParamsResponse = {
    $type: "cosmos.slashing.v1beta1.QueryParamsResponse",
    encode(message, writer = new BinaryWriter()) {
        if (message.params !== undefined) {
            Params.encode(message.params, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryParamsResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.params = Params.decode(reader, reader.uint32());
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
        return { params: isSet(object.params) ? Params.fromJSON(object.params) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.params !== undefined) {
            obj.params = Params.toJSON(message.params);
        }
        return obj;
    },
    create(base) {
        return QueryParamsResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryParamsResponse();
        message.params = object.params !== undefined && object.params !== null ? Params.fromPartial(object.params) : undefined;
        return message;
    }
};
export const QuerySigningInfoRequest = {
    $type: "cosmos.slashing.v1beta1.QuerySigningInfoRequest",
    encode(message, writer = new BinaryWriter()) {
        if (message.cons_address !== "") {
            writer.uint32(10).string(message.cons_address);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
        return QuerySigningInfoRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQuerySigningInfoRequest();
        message.cons_address = object.cons_address ?? "";
        return message;
    }
};
export const QuerySigningInfoResponse = {
    $type: "cosmos.slashing.v1beta1.QuerySigningInfoResponse",
    encode(message, writer = new BinaryWriter()) {
        if (message.val_signing_info !== undefined) {
            ValidatorSigningInfo.encode(message.val_signing_info, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQuerySigningInfoResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.val_signing_info = ValidatorSigningInfo.decode(reader, reader.uint32());
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
            val_signing_info: isSet(object.val_signing_info) ? ValidatorSigningInfo.fromJSON(object.val_signing_info) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.val_signing_info !== undefined) {
            obj.val_signing_info = ValidatorSigningInfo.toJSON(message.val_signing_info);
        }
        return obj;
    },
    create(base) {
        return QuerySigningInfoResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQuerySigningInfoResponse();
        message.val_signing_info =
            object.val_signing_info !== undefined && object.val_signing_info !== null ? ValidatorSigningInfo.fromPartial(object.val_signing_info) : undefined;
        return message;
    }
};
export const QuerySigningInfosRequest = {
    $type: "cosmos.slashing.v1beta1.QuerySigningInfosRequest",
    encode(message, writer = new BinaryWriter()) {
        if (message.pagination !== undefined) {
            PageRequest.encode(message.pagination, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQuerySigningInfosRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.pagination = PageRequest.decode(reader, reader.uint32());
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
        return { pagination: isSet(object.pagination) ? PageRequest.fromJSON(object.pagination) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.pagination !== undefined) {
            obj.pagination = PageRequest.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return QuerySigningInfosRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQuerySigningInfosRequest();
        message.pagination = object.pagination !== undefined && object.pagination !== null ? PageRequest.fromPartial(object.pagination) : undefined;
        return message;
    }
};
export const QuerySigningInfosResponse = {
    $type: "cosmos.slashing.v1beta1.QuerySigningInfosResponse",
    encode(message, writer = new BinaryWriter()) {
        for (const v of message.info) {
            ValidatorSigningInfo.encode(v, writer.uint32(10).fork()).join();
        }
        if (message.pagination !== undefined) {
            PageResponse.encode(message.pagination, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQuerySigningInfosResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.info.push(ValidatorSigningInfo.decode(reader, reader.uint32()));
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.pagination = PageResponse.decode(reader, reader.uint32());
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
            info: globalThis.Array.isArray(object?.info) ? object.info.map((e) => ValidatorSigningInfo.fromJSON(e)) : [],
            pagination: isSet(object.pagination) ? PageResponse.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.info?.length) {
            obj.info = message.info.map((e) => ValidatorSigningInfo.toJSON(e));
        }
        if (message.pagination !== undefined) {
            obj.pagination = PageResponse.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return QuerySigningInfosResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQuerySigningInfosResponse();
        message.info = object.info?.map((e) => ValidatorSigningInfo.fromPartial(e)) || [];
        message.pagination = object.pagination !== undefined && object.pagination !== null ? PageResponse.fromPartial(object.pagination) : undefined;
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
export const registry = [
    ["/cosmos.slashing.v1beta1.QueryParamsRequest", QueryParamsRequest],
    ["/cosmos.slashing.v1beta1.QueryParamsResponse", QueryParamsResponse],
    ["/cosmos.slashing.v1beta1.QuerySigningInfoRequest", QuerySigningInfoRequest],
    ["/cosmos.slashing.v1beta1.QuerySigningInfoResponse", QuerySigningInfoResponse],
    ["/cosmos.slashing.v1beta1.QuerySigningInfosRequest", QuerySigningInfosRequest],
    ["/cosmos.slashing.v1beta1.QuerySigningInfosResponse", QuerySigningInfosResponse]
];
export const aminoConverters = {
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
