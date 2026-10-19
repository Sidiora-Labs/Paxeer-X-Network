import { BinaryReader, BinaryWriter } from "@bufbuild/protobuf/wire";
import { ParamChange } from "./params";
export const QueryParamsRequest = {
    $type: "cosmos.params.v1beta1.QueryParamsRequest",
    encode(message, writer = new BinaryWriter()) {
        if (message.subspace !== "") {
            writer.uint32(10).string(message.subspace);
        }
        if (message.key !== "") {
            writer.uint32(18).string(message.key);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryParamsRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.subspace = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.key = reader.string();
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
            subspace: isSet(object.subspace) ? globalThis.String(object.subspace) : "",
            key: isSet(object.key) ? globalThis.String(object.key) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.subspace !== "") {
            obj.subspace = message.subspace;
        }
        if (message.key !== "") {
            obj.key = message.key;
        }
        return obj;
    },
    create(base) {
        return QueryParamsRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryParamsRequest();
        message.subspace = object.subspace ?? "";
        message.key = object.key ?? "";
        return message;
    }
};
export const QueryParamsResponse = {
    $type: "cosmos.params.v1beta1.QueryParamsResponse",
    encode(message, writer = new BinaryWriter()) {
        if (message.param !== undefined) {
            ParamChange.encode(message.param, writer.uint32(10).fork()).join();
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
                    message.param = ParamChange.decode(reader, reader.uint32());
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
        return { param: isSet(object.param) ? ParamChange.fromJSON(object.param) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.param !== undefined) {
            obj.param = ParamChange.toJSON(message.param);
        }
        return obj;
    },
    create(base) {
        return QueryParamsResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryParamsResponse();
        message.param = object.param !== undefined && object.param !== null ? ParamChange.fromPartial(object.param) : undefined;
        return message;
    }
};
function createBaseQueryParamsRequest() {
    return { subspace: "", key: "" };
}
function createBaseQueryParamsResponse() {
    return { param: undefined };
}
function isSet(value) {
    return value !== null && value !== undefined;
}
export const registry = [
    ["/cosmos.params.v1beta1.QueryParamsRequest", QueryParamsRequest],
    ["/cosmos.params.v1beta1.QueryParamsResponse", QueryParamsResponse]
];
export const aminoConverters = {
    "/cosmos.params.v1beta1.QueryParamsRequest": {
        aminoType: "cosmos-sdk/QueryParamsRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.params.v1beta1.QueryParamsResponse": {
        aminoType: "cosmos-sdk/QueryParamsResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
