import { BinaryReader, BinaryWriter } from "@bufbuild/protobuf/wire";
import { AllowList, Metadata } from "../cosmos/bank/v1beta1/bank";
import { DenomAuthorityMetadata } from "./authorityMetadata";
import { Params } from "./params";
export const QueryParamsRequest = {
    $type: "paxprotocol.paxchain.tokenfactory.QueryParamsRequest",
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
    $type: "paxprotocol.paxchain.tokenfactory.QueryParamsResponse",
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
export const QueryDenomAuthorityMetadataRequest = {
    $type: "paxprotocol.paxchain.tokenfactory.QueryDenomAuthorityMetadataRequest",
    encode(message, writer = new BinaryWriter()) {
        if (message.denom !== "") {
            writer.uint32(10).string(message.denom);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
        return QueryDenomAuthorityMetadataRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDenomAuthorityMetadataRequest();
        message.denom = object.denom ?? "";
        return message;
    }
};
export const QueryDenomAuthorityMetadataResponse = {
    $type: "paxprotocol.paxchain.tokenfactory.QueryDenomAuthorityMetadataResponse",
    encode(message, writer = new BinaryWriter()) {
        if (message.authority_metadata !== undefined) {
            DenomAuthorityMetadata.encode(message.authority_metadata, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryDenomAuthorityMetadataResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.authority_metadata = DenomAuthorityMetadata.decode(reader, reader.uint32());
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
            authority_metadata: isSet(object.authority_metadata) ? DenomAuthorityMetadata.fromJSON(object.authority_metadata) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.authority_metadata !== undefined) {
            obj.authority_metadata = DenomAuthorityMetadata.toJSON(message.authority_metadata);
        }
        return obj;
    },
    create(base) {
        return QueryDenomAuthorityMetadataResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDenomAuthorityMetadataResponse();
        message.authority_metadata =
            object.authority_metadata !== undefined && object.authority_metadata !== null ? DenomAuthorityMetadata.fromPartial(object.authority_metadata) : undefined;
        return message;
    }
};
export const QueryDenomsFromCreatorRequest = {
    $type: "paxprotocol.paxchain.tokenfactory.QueryDenomsFromCreatorRequest",
    encode(message, writer = new BinaryWriter()) {
        if (message.creator !== "") {
            writer.uint32(10).string(message.creator);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
        return QueryDenomsFromCreatorRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDenomsFromCreatorRequest();
        message.creator = object.creator ?? "";
        return message;
    }
};
export const QueryDenomsFromCreatorResponse = {
    $type: "paxprotocol.paxchain.tokenfactory.QueryDenomsFromCreatorResponse",
    encode(message, writer = new BinaryWriter()) {
        for (const v of message.denoms) {
            writer.uint32(10).string(v);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
        return QueryDenomsFromCreatorResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDenomsFromCreatorResponse();
        message.denoms = object.denoms?.map((e) => e) || [];
        return message;
    }
};
export const QueryDenomMetadataRequest = {
    $type: "paxprotocol.paxchain.tokenfactory.QueryDenomMetadataRequest",
    encode(message, writer = new BinaryWriter()) {
        if (message.denom !== "") {
            writer.uint32(10).string(message.denom);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
        return QueryDenomMetadataRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDenomMetadataRequest();
        message.denom = object.denom ?? "";
        return message;
    }
};
export const QueryDenomMetadataResponse = {
    $type: "paxprotocol.paxchain.tokenfactory.QueryDenomMetadataResponse",
    encode(message, writer = new BinaryWriter()) {
        if (message.metadata !== undefined) {
            Metadata.encode(message.metadata, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryDenomMetadataResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.metadata = Metadata.decode(reader, reader.uint32());
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
        return { metadata: isSet(object.metadata) ? Metadata.fromJSON(object.metadata) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.metadata !== undefined) {
            obj.metadata = Metadata.toJSON(message.metadata);
        }
        return obj;
    },
    create(base) {
        return QueryDenomMetadataResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDenomMetadataResponse();
        message.metadata = object.metadata !== undefined && object.metadata !== null ? Metadata.fromPartial(object.metadata) : undefined;
        return message;
    }
};
export const QueryDenomAllowListRequest = {
    $type: "paxprotocol.paxchain.tokenfactory.QueryDenomAllowListRequest",
    encode(message, writer = new BinaryWriter()) {
        if (message.denom !== "") {
            writer.uint32(10).string(message.denom);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
        return QueryDenomAllowListRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDenomAllowListRequest();
        message.denom = object.denom ?? "";
        return message;
    }
};
export const QueryDenomAllowListResponse = {
    $type: "paxprotocol.paxchain.tokenfactory.QueryDenomAllowListResponse",
    encode(message, writer = new BinaryWriter()) {
        if (message.allow_list !== undefined) {
            AllowList.encode(message.allow_list, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryDenomAllowListResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.allow_list = AllowList.decode(reader, reader.uint32());
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
        return { allow_list: isSet(object.allow_list) ? AllowList.fromJSON(object.allow_list) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.allow_list !== undefined) {
            obj.allow_list = AllowList.toJSON(message.allow_list);
        }
        return obj;
    },
    create(base) {
        return QueryDenomAllowListResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDenomAllowListResponse();
        message.allow_list = object.allow_list !== undefined && object.allow_list !== null ? AllowList.fromPartial(object.allow_list) : undefined;
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
export const registry = [
    ["/paxprotocol.paxchain.tokenfactory.QueryParamsRequest", QueryParamsRequest],
    ["/paxprotocol.paxchain.tokenfactory.QueryParamsResponse", QueryParamsResponse],
    ["/paxprotocol.paxchain.tokenfactory.QueryDenomAuthorityMetadataRequest", QueryDenomAuthorityMetadataRequest],
    ["/paxprotocol.paxchain.tokenfactory.QueryDenomAuthorityMetadataResponse", QueryDenomAuthorityMetadataResponse],
    ["/paxprotocol.paxchain.tokenfactory.QueryDenomsFromCreatorRequest", QueryDenomsFromCreatorRequest],
    ["/paxprotocol.paxchain.tokenfactory.QueryDenomsFromCreatorResponse", QueryDenomsFromCreatorResponse],
    ["/paxprotocol.paxchain.tokenfactory.QueryDenomMetadataRequest", QueryDenomMetadataRequest],
    ["/paxprotocol.paxchain.tokenfactory.QueryDenomMetadataResponse", QueryDenomMetadataResponse],
    ["/paxprotocol.paxchain.tokenfactory.QueryDenomAllowListRequest", QueryDenomAllowListRequest],
    ["/paxprotocol.paxchain.tokenfactory.QueryDenomAllowListResponse", QueryDenomAllowListResponse]
];
export const aminoConverters = {
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
