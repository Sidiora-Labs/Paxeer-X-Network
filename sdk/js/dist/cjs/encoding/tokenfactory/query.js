"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.QueryDenomAllowListResponse = exports.QueryDenomAllowListRequest = exports.QueryDenomMetadataResponse = exports.QueryDenomMetadataRequest = exports.QueryDenomsFromCreatorResponse = exports.QueryDenomsFromCreatorRequest = exports.QueryDenomAuthorityMetadataResponse = exports.QueryDenomAuthorityMetadataRequest = exports.QueryParamsResponse = exports.QueryParamsRequest = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
const bank_1 = require("../cosmos/bank/v1beta1/bank");
const authorityMetadata_1 = require("./authorityMetadata");
const params_1 = require("./params");
exports.QueryParamsRequest = {
    $type: "paxprotocol.paxchain.tokenfactory.QueryParamsRequest",
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
    $type: "paxprotocol.paxchain.tokenfactory.QueryParamsResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.params !== undefined) {
            params_1.Params.encode(message.params, writer.uint32(10).fork()).join();
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
                    message.params = params_1.Params.decode(reader, reader.uint32());
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
        return { params: isSet(object.params) ? params_1.Params.fromJSON(object.params) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.params !== undefined) {
            obj.params = params_1.Params.toJSON(message.params);
        }
        return obj;
    },
    create(base) {
        return exports.QueryParamsResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryParamsResponse();
        message.params = object.params !== undefined && object.params !== null ? params_1.Params.fromPartial(object.params) : undefined;
        return message;
    }
};
exports.QueryDenomAuthorityMetadataRequest = {
    $type: "paxprotocol.paxchain.tokenfactory.QueryDenomAuthorityMetadataRequest",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.denom !== "") {
            writer.uint32(10).string(message.denom);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryDenomAuthorityMetadataRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.denom = reader.string();
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
        return { denom: isSet(object.denom) ? globalThis.String(object.denom) : "" };
    },
    toJSON(message) {
        const obj = {};
        if (message.denom !== "") {
            obj.denom = message.denom;
        }
        return obj;
    },
    create(base) {
        return exports.QueryDenomAuthorityMetadataRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDenomAuthorityMetadataRequest();
        message.denom = object.denom ?? "";
        return message;
    }
};
exports.QueryDenomAuthorityMetadataResponse = {
    $type: "paxprotocol.paxchain.tokenfactory.QueryDenomAuthorityMetadataResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.authority_metadata !== undefined) {
            authorityMetadata_1.DenomAuthorityMetadata.encode(message.authority_metadata, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryDenomAuthorityMetadataResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.authority_metadata = authorityMetadata_1.DenomAuthorityMetadata.decode(reader, reader.uint32());
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
            authority_metadata: isSet(object.authority_metadata) ? authorityMetadata_1.DenomAuthorityMetadata.fromJSON(object.authority_metadata) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.authority_metadata !== undefined) {
            obj.authority_metadata = authorityMetadata_1.DenomAuthorityMetadata.toJSON(message.authority_metadata);
        }
        return obj;
    },
    create(base) {
        return exports.QueryDenomAuthorityMetadataResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDenomAuthorityMetadataResponse();
        message.authority_metadata =
            object.authority_metadata !== undefined && object.authority_metadata !== null ? authorityMetadata_1.DenomAuthorityMetadata.fromPartial(object.authority_metadata) : undefined;
        return message;
    }
};
exports.QueryDenomsFromCreatorRequest = {
    $type: "paxprotocol.paxchain.tokenfactory.QueryDenomsFromCreatorRequest",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.creator !== "") {
            writer.uint32(10).string(message.creator);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryDenomsFromCreatorRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.creator = reader.string();
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
        return { creator: isSet(object.creator) ? globalThis.String(object.creator) : "" };
    },
    toJSON(message) {
        const obj = {};
        if (message.creator !== "") {
            obj.creator = message.creator;
        }
        return obj;
    },
    create(base) {
        return exports.QueryDenomsFromCreatorRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDenomsFromCreatorRequest();
        message.creator = object.creator ?? "";
        return message;
    }
};
exports.QueryDenomsFromCreatorResponse = {
    $type: "paxprotocol.paxchain.tokenfactory.QueryDenomsFromCreatorResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.denoms) {
            writer.uint32(10).string(v);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryDenomsFromCreatorResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.denoms.push(reader.string());
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
            denoms: globalThis.Array.isArray(object?.denoms) ? object.denoms.map((e) => globalThis.String(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.denoms?.length) {
            obj.denoms = message.denoms;
        }
        return obj;
    },
    create(base) {
        return exports.QueryDenomsFromCreatorResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDenomsFromCreatorResponse();
        message.denoms = object.denoms?.map((e) => e) || [];
        return message;
    }
};
exports.QueryDenomMetadataRequest = {
    $type: "paxprotocol.paxchain.tokenfactory.QueryDenomMetadataRequest",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.denom !== "") {
            writer.uint32(10).string(message.denom);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryDenomMetadataRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.denom = reader.string();
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
        return { denom: isSet(object.denom) ? globalThis.String(object.denom) : "" };
    },
    toJSON(message) {
        const obj = {};
        if (message.denom !== "") {
            obj.denom = message.denom;
        }
        return obj;
    },
    create(base) {
        return exports.QueryDenomMetadataRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDenomMetadataRequest();
        message.denom = object.denom ?? "";
        return message;
    }
};
exports.QueryDenomMetadataResponse = {
    $type: "paxprotocol.paxchain.tokenfactory.QueryDenomMetadataResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.metadata !== undefined) {
            bank_1.Metadata.encode(message.metadata, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryDenomMetadataResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.metadata = bank_1.Metadata.decode(reader, reader.uint32());
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
        return { metadata: isSet(object.metadata) ? bank_1.Metadata.fromJSON(object.metadata) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.metadata !== undefined) {
            obj.metadata = bank_1.Metadata.toJSON(message.metadata);
        }
        return obj;
    },
    create(base) {
        return exports.QueryDenomMetadataResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDenomMetadataResponse();
        message.metadata = object.metadata !== undefined && object.metadata !== null ? bank_1.Metadata.fromPartial(object.metadata) : undefined;
        return message;
    }
};
exports.QueryDenomAllowListRequest = {
    $type: "paxprotocol.paxchain.tokenfactory.QueryDenomAllowListRequest",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.denom !== "") {
            writer.uint32(10).string(message.denom);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryDenomAllowListRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.denom = reader.string();
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
        return { denom: isSet(object.denom) ? globalThis.String(object.denom) : "" };
    },
    toJSON(message) {
        const obj = {};
        if (message.denom !== "") {
            obj.denom = message.denom;
        }
        return obj;
    },
    create(base) {
        return exports.QueryDenomAllowListRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDenomAllowListRequest();
        message.denom = object.denom ?? "";
        return message;
    }
};
exports.QueryDenomAllowListResponse = {
    $type: "paxprotocol.paxchain.tokenfactory.QueryDenomAllowListResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.allow_list !== undefined) {
            bank_1.AllowList.encode(message.allow_list, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryDenomAllowListResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.allow_list = bank_1.AllowList.decode(reader, reader.uint32());
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
        return { allow_list: isSet(object.allow_list) ? bank_1.AllowList.fromJSON(object.allow_list) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.allow_list !== undefined) {
            obj.allow_list = bank_1.AllowList.toJSON(message.allow_list);
        }
        return obj;
    },
    create(base) {
        return exports.QueryDenomAllowListResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDenomAllowListResponse();
        message.allow_list = object.allow_list !== undefined && object.allow_list !== null ? bank_1.AllowList.fromPartial(object.allow_list) : undefined;
        return message;
    }
};
function createBaseQueryParamsRequest() {
    return {};
}
function createBaseQueryParamsResponse() {
    return { params: undefined };
}
function createBaseQueryDenomAuthorityMetadataRequest() {
    return { denom: "" };
}
function createBaseQueryDenomAuthorityMetadataResponse() {
    return { authority_metadata: undefined };
}
function createBaseQueryDenomsFromCreatorRequest() {
    return { creator: "" };
}
function createBaseQueryDenomsFromCreatorResponse() {
    return { denoms: [] };
}
function createBaseQueryDenomMetadataRequest() {
    return { denom: "" };
}
function createBaseQueryDenomMetadataResponse() {
    return { metadata: undefined };
}
function createBaseQueryDenomAllowListRequest() {
    return { denom: "" };
}
function createBaseQueryDenomAllowListResponse() {
    return { allow_list: undefined };
}
function isSet(value) {
    return value !== null && value !== undefined;
}
exports.registry = [
    ["/paxprotocol.paxchain.tokenfactory.QueryParamsRequest", exports.QueryParamsRequest],
    ["/paxprotocol.paxchain.tokenfactory.QueryParamsResponse", exports.QueryParamsResponse],
    ["/paxprotocol.paxchain.tokenfactory.QueryDenomAuthorityMetadataRequest", exports.QueryDenomAuthorityMetadataRequest],
    ["/paxprotocol.paxchain.tokenfactory.QueryDenomAuthorityMetadataResponse", exports.QueryDenomAuthorityMetadataResponse],
    ["/paxprotocol.paxchain.tokenfactory.QueryDenomsFromCreatorRequest", exports.QueryDenomsFromCreatorRequest],
    ["/paxprotocol.paxchain.tokenfactory.QueryDenomsFromCreatorResponse", exports.QueryDenomsFromCreatorResponse],
    ["/paxprotocol.paxchain.tokenfactory.QueryDenomMetadataRequest", exports.QueryDenomMetadataRequest],
    ["/paxprotocol.paxchain.tokenfactory.QueryDenomMetadataResponse", exports.QueryDenomMetadataResponse],
    ["/paxprotocol.paxchain.tokenfactory.QueryDenomAllowListRequest", exports.QueryDenomAllowListRequest],
    ["/paxprotocol.paxchain.tokenfactory.QueryDenomAllowListResponse", exports.QueryDenomAllowListResponse]
];
exports.aminoConverters = {
    "/paxprotocol.paxchain.tokenfactory.QueryParamsRequest": {
        aminoType: "tokenfactory/QueryParamsRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.tokenfactory.QueryParamsResponse": {
        aminoType: "tokenfactory/QueryParamsResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.tokenfactory.QueryDenomAuthorityMetadataRequest": {
        aminoType: "tokenfactory/QueryDenomAuthorityMetadataRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.tokenfactory.QueryDenomAuthorityMetadataResponse": {
        aminoType: "tokenfactory/QueryDenomAuthorityMetadataResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.tokenfactory.QueryDenomsFromCreatorRequest": {
        aminoType: "tokenfactory/QueryDenomsFromCreatorRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.tokenfactory.QueryDenomsFromCreatorResponse": {
        aminoType: "tokenfactory/QueryDenomsFromCreatorResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.tokenfactory.QueryDenomMetadataRequest": {
        aminoType: "tokenfactory/QueryDenomMetadataRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.tokenfactory.QueryDenomMetadataResponse": {
        aminoType: "tokenfactory/QueryDenomMetadataResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.tokenfactory.QueryDenomAllowListRequest": {
        aminoType: "tokenfactory/QueryDenomAllowListRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.tokenfactory.QueryDenomAllowListResponse": {
        aminoType: "tokenfactory/QueryDenomAllowListResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
