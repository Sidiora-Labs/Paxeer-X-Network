import { BinaryReader, BinaryWriter } from "@bufbuild/protobuf/wire";
import { Any } from "../../../google/protobuf/any";
import { PageRequest, PageResponse } from "../../base/query/v1beta1/pagination";
import { Params } from "./auth";
export const QueryAccountsRequest = {
    $type: "cosmos.auth.v1beta1.QueryAccountsRequest",
    encode(message, writer = new BinaryWriter()) {
        if (message.pagination !== undefined) {
            PageRequest.encode(message.pagination, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryAccountsRequest();
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
        return QueryAccountsRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryAccountsRequest();
        message.pagination = object.pagination !== undefined && object.pagination !== null ? PageRequest.fromPartial(object.pagination) : undefined;
        return message;
    }
};
export const QueryAccountsResponse = {
    $type: "cosmos.auth.v1beta1.QueryAccountsResponse",
    encode(message, writer = new BinaryWriter()) {
        for (const v of message.accounts) {
            Any.encode(v, writer.uint32(10).fork()).join();
        }
        if (message.pagination !== undefined) {
            PageResponse.encode(message.pagination, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryAccountsResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.accounts.push(Any.decode(reader, reader.uint32()));
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
            accounts: globalThis.Array.isArray(object?.accounts) ? object.accounts.map((e) => Any.fromJSON(e)) : [],
            pagination: isSet(object.pagination) ? PageResponse.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.accounts?.length) {
            obj.accounts = message.accounts.map((e) => Any.toJSON(e));
        }
        if (message.pagination !== undefined) {
            obj.pagination = PageResponse.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return QueryAccountsResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryAccountsResponse();
        message.accounts = object.accounts?.map((e) => Any.fromPartial(e)) || [];
        message.pagination = object.pagination !== undefined && object.pagination !== null ? PageResponse.fromPartial(object.pagination) : undefined;
        return message;
    }
};
export const QueryAccountRequest = {
    $type: "cosmos.auth.v1beta1.QueryAccountRequest",
    encode(message, writer = new BinaryWriter()) {
        if (message.address !== "") {
            writer.uint32(10).string(message.address);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryAccountRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.address = reader.string();
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
        return { address: isSet(object.address) ? globalThis.String(object.address) : "" };
    },
    toJSON(message) {
        const obj = {};
        if (message.address !== "") {
            obj.address = message.address;
        }
        return obj;
    },
    create(base) {
        return QueryAccountRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryAccountRequest();
        message.address = object.address ?? "";
        return message;
    }
};
export const QueryAccountResponse = {
    $type: "cosmos.auth.v1beta1.QueryAccountResponse",
    encode(message, writer = new BinaryWriter()) {
        if (message.account !== undefined) {
            Any.encode(message.account, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryAccountResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.account = Any.decode(reader, reader.uint32());
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
        return { account: isSet(object.account) ? Any.fromJSON(object.account) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.account !== undefined) {
            obj.account = Any.toJSON(message.account);
        }
        return obj;
    },
    create(base) {
        return QueryAccountResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryAccountResponse();
        message.account = object.account !== undefined && object.account !== null ? Any.fromPartial(object.account) : undefined;
        return message;
    }
};
export const QueryParamsRequest = {
    $type: "cosmos.auth.v1beta1.QueryParamsRequest",
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
    $type: "cosmos.auth.v1beta1.QueryParamsResponse",
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
export const QueryNextAccountNumberRequest = {
    $type: "cosmos.auth.v1beta1.QueryNextAccountNumberRequest",
    encode(_, writer = new BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryNextAccountNumberRequest();
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
        return QueryNextAccountNumberRequest.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseQueryNextAccountNumberRequest();
        return message;
    }
};
export const QueryNextAccountNumberResponse = {
    $type: "cosmos.auth.v1beta1.QueryNextAccountNumberResponse",
    encode(message, writer = new BinaryWriter()) {
        if (message.count !== 0) {
            writer.uint32(8).uint64(message.count);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryNextAccountNumberResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.count = longToNumber(reader.uint64());
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
        return { count: isSet(object.count) ? globalThis.Number(object.count) : 0 };
    },
    toJSON(message) {
        const obj = {};
        if (message.count !== 0) {
            obj.count = Math.round(message.count);
        }
        return obj;
    },
    create(base) {
        return QueryNextAccountNumberResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryNextAccountNumberResponse();
        message.count = object.count ?? 0;
        return message;
    }
};
function createBaseQueryAccountsRequest() {
    return { pagination: undefined };
}
function createBaseQueryAccountsResponse() {
    return { accounts: [], pagination: undefined };
}
function createBaseQueryAccountRequest() {
    return { address: "" };
}
function createBaseQueryAccountResponse() {
    return { account: undefined };
}
function createBaseQueryParamsRequest() {
    return {};
}
function createBaseQueryParamsResponse() {
    return { params: undefined };
}
function createBaseQueryNextAccountNumberRequest() {
    return {};
}
function createBaseQueryNextAccountNumberResponse() {
    return { count: 0 };
}
function longToNumber(int64) {
    const num = globalThis.Number(int64.toString());
    if (num > globalThis.Number.MAX_SAFE_INTEGER) {
        throw new globalThis.Error("Value is larger than Number.MAX_SAFE_INTEGER");
    }
    if (num < globalThis.Number.MIN_SAFE_INTEGER) {
        throw new globalThis.Error("Value is smaller than Number.MIN_SAFE_INTEGER");
    }
    return num;
}
function isSet(value) {
    return value !== null && value !== undefined;
}
export const registry = [
    ["/cosmos.auth.v1beta1.QueryAccountsRequest", QueryAccountsRequest],
    ["/cosmos.auth.v1beta1.QueryAccountsResponse", QueryAccountsResponse],
    ["/cosmos.auth.v1beta1.QueryAccountRequest", QueryAccountRequest],
    ["/cosmos.auth.v1beta1.QueryAccountResponse", QueryAccountResponse],
    ["/cosmos.auth.v1beta1.QueryParamsRequest", QueryParamsRequest],
    ["/cosmos.auth.v1beta1.QueryParamsResponse", QueryParamsResponse],
    ["/cosmos.auth.v1beta1.QueryNextAccountNumberRequest", QueryNextAccountNumberRequest],
    ["/cosmos.auth.v1beta1.QueryNextAccountNumberResponse", QueryNextAccountNumberResponse]
];
export const aminoConverters = {
    "/cosmos.auth.v1beta1.QueryAccountsRequest": {
        aminoType: "cosmos-sdk/QueryAccountsRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.auth.v1beta1.QueryAccountsResponse": {
        aminoType: "cosmos-sdk/QueryAccountsResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.auth.v1beta1.QueryAccountRequest": {
        aminoType: "cosmos-sdk/QueryAccountRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.auth.v1beta1.QueryAccountResponse": {
        aminoType: "cosmos-sdk/QueryAccountResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.auth.v1beta1.QueryParamsRequest": {
        aminoType: "cosmos-sdk/QueryParamsRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.auth.v1beta1.QueryParamsResponse": {
        aminoType: "cosmos-sdk/QueryParamsResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.auth.v1beta1.QueryNextAccountNumberRequest": {
        aminoType: "cosmos-sdk/QueryNextAccountNumberRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.auth.v1beta1.QueryNextAccountNumberResponse": {
        aminoType: "cosmos-sdk/QueryNextAccountNumberResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
