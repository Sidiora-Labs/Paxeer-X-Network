"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.QueryAnnualProvisionsResponse = exports.QueryAnnualProvisionsRequest = exports.QueryInflationResponse = exports.QueryInflationRequest = exports.QueryParamsResponse = exports.QueryParamsRequest = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
const mint_1 = require("./mint");
exports.QueryParamsRequest = {
    $type: "cosmos.mint.v1beta1.QueryParamsRequest",
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
    $type: "cosmos.mint.v1beta1.QueryParamsResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.params !== undefined) {
            mint_1.Params.encode(message.params, writer.uint32(10).fork()).join();
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
                    message.params = mint_1.Params.decode(reader, reader.uint32());
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
        return { params: isSet(object.params) ? mint_1.Params.fromJSON(object.params) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.params !== undefined) {
            obj.params = mint_1.Params.toJSON(message.params);
        }
        return obj;
    },
    create(base) {
        return exports.QueryParamsResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryParamsResponse();
        message.params = object.params !== undefined && object.params !== null ? mint_1.Params.fromPartial(object.params) : undefined;
        return message;
    }
};
exports.QueryInflationRequest = {
    $type: "cosmos.mint.v1beta1.QueryInflationRequest",
    encode(_, writer = new wire_1.BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryInflationRequest();
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
        return exports.QueryInflationRequest.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseQueryInflationRequest();
        return message;
    }
};
exports.QueryInflationResponse = {
    $type: "cosmos.mint.v1beta1.QueryInflationResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.inflation.length !== 0) {
            writer.uint32(10).bytes(message.inflation);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryInflationResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.inflation = reader.bytes();
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
        return { inflation: isSet(object.inflation) ? bytesFromBase64(object.inflation) : new Uint8Array(0) };
    },
    toJSON(message) {
        const obj = {};
        if (message.inflation.length !== 0) {
            obj.inflation = base64FromBytes(message.inflation);
        }
        return obj;
    },
    create(base) {
        return exports.QueryInflationResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryInflationResponse();
        message.inflation = object.inflation ?? new Uint8Array(0);
        return message;
    }
};
exports.QueryAnnualProvisionsRequest = {
    $type: "cosmos.mint.v1beta1.QueryAnnualProvisionsRequest",
    encode(_, writer = new wire_1.BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryAnnualProvisionsRequest();
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
        return exports.QueryAnnualProvisionsRequest.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseQueryAnnualProvisionsRequest();
        return message;
    }
};
exports.QueryAnnualProvisionsResponse = {
    $type: "cosmos.mint.v1beta1.QueryAnnualProvisionsResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.annual_provisions.length !== 0) {
            writer.uint32(10).bytes(message.annual_provisions);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryAnnualProvisionsResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.annual_provisions = reader.bytes();
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
            annual_provisions: isSet(object.annual_provisions) ? bytesFromBase64(object.annual_provisions) : new Uint8Array(0)
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.annual_provisions.length !== 0) {
            obj.annual_provisions = base64FromBytes(message.annual_provisions);
        }
        return obj;
    },
    create(base) {
        return exports.QueryAnnualProvisionsResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryAnnualProvisionsResponse();
        message.annual_provisions = object.annual_provisions ?? new Uint8Array(0);
        return message;
    }
};
function createBaseQueryParamsRequest() {
    return {};
}
function createBaseQueryParamsResponse() {
    return { params: undefined };
}
function createBaseQueryInflationRequest() {
    return {};
}
function createBaseQueryInflationResponse() {
    return { inflation: new Uint8Array(0) };
}
function createBaseQueryAnnualProvisionsRequest() {
    return {};
}
function createBaseQueryAnnualProvisionsResponse() {
    return { annual_provisions: new Uint8Array(0) };
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
    ["/cosmos.mint.v1beta1.QueryParamsRequest", exports.QueryParamsRequest],
    ["/cosmos.mint.v1beta1.QueryParamsResponse", exports.QueryParamsResponse],
    ["/cosmos.mint.v1beta1.QueryInflationRequest", exports.QueryInflationRequest],
    ["/cosmos.mint.v1beta1.QueryInflationResponse", exports.QueryInflationResponse],
    ["/cosmos.mint.v1beta1.QueryAnnualProvisionsRequest", exports.QueryAnnualProvisionsRequest],
    ["/cosmos.mint.v1beta1.QueryAnnualProvisionsResponse", exports.QueryAnnualProvisionsResponse]
];
exports.aminoConverters = {
    "/cosmos.mint.v1beta1.QueryParamsRequest": {
        aminoType: "cosmos-sdk/QueryParamsRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.mint.v1beta1.QueryParamsResponse": {
        aminoType: "cosmos-sdk/QueryParamsResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.mint.v1beta1.QueryInflationRequest": {
        aminoType: "cosmos-sdk/QueryInflationRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.mint.v1beta1.QueryInflationResponse": {
        aminoType: "cosmos-sdk/QueryInflationResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.mint.v1beta1.QueryAnnualProvisionsRequest": {
        aminoType: "cosmos-sdk/QueryAnnualProvisionsRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.mint.v1beta1.QueryAnnualProvisionsResponse": {
        aminoType: "cosmos-sdk/QueryAnnualProvisionsResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
