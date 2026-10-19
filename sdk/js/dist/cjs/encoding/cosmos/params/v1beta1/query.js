"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.QueryParamsResponse = exports.QueryParamsRequest = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
const params_1 = require("./params");
exports.QueryParamsRequest = {
    $type: "cosmos.params.v1beta1.QueryParamsRequest",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.subspace !== "") {
            writer.uint32(10).string(message.subspace);
        }
        if (message.key !== "") {
            writer.uint32(18).string(message.key);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
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
        return exports.QueryParamsRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryParamsRequest();
        message.subspace = object.subspace ?? "";
        message.key = object.key ?? "";
        return message;
    }
};
exports.QueryParamsResponse = {
    $type: "cosmos.params.v1beta1.QueryParamsResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.param !== undefined) {
            params_1.ParamChange.encode(message.param, writer.uint32(10).fork()).join();
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
                    message.param = params_1.ParamChange.decode(reader, reader.uint32());
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
        return { param: isSet(object.param) ? params_1.ParamChange.fromJSON(object.param) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.param !== undefined) {
            obj.param = params_1.ParamChange.toJSON(message.param);
        }
        return obj;
    },
    create(base) {
        return exports.QueryParamsResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryParamsResponse();
        message.param = object.param !== undefined && object.param !== null ? params_1.ParamChange.fromPartial(object.param) : undefined;
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
exports.registry = [
    ["/cosmos.params.v1beta1.QueryParamsRequest", exports.QueryParamsRequest],
    ["/cosmos.params.v1beta1.QueryParamsResponse", exports.QueryParamsResponse]
];
exports.aminoConverters = {
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
