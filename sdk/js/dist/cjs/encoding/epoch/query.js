"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.QueryEpochResponse = exports.QueryEpochRequest = exports.QueryParamsResponse = exports.QueryParamsRequest = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
const epoch_1 = require("./epoch");
const params_1 = require("./params");
exports.QueryParamsRequest = {
    $type: "paxprotocol.paxchain.epoch.QueryParamsRequest",
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
    $type: "paxprotocol.paxchain.epoch.QueryParamsResponse",
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
exports.QueryEpochRequest = {
    $type: "paxprotocol.paxchain.epoch.QueryEpochRequest",
    encode(_, writer = new wire_1.BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryEpochRequest();
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
        return exports.QueryEpochRequest.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseQueryEpochRequest();
        return message;
    }
};
exports.QueryEpochResponse = {
    $type: "paxprotocol.paxchain.epoch.QueryEpochResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.epoch !== undefined) {
            epoch_1.Epoch.encode(message.epoch, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryEpochResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.epoch = epoch_1.Epoch.decode(reader, reader.uint32());
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
        return { epoch: isSet(object.epoch) ? epoch_1.Epoch.fromJSON(object.epoch) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.epoch !== undefined) {
            obj.epoch = epoch_1.Epoch.toJSON(message.epoch);
        }
        return obj;
    },
    create(base) {
        return exports.QueryEpochResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryEpochResponse();
        message.epoch = object.epoch !== undefined && object.epoch !== null ? epoch_1.Epoch.fromPartial(object.epoch) : undefined;
        return message;
    }
};
function createBaseQueryParamsRequest() {
    return {};
}
function createBaseQueryParamsResponse() {
    return { params: undefined };
}
function createBaseQueryEpochRequest() {
    return {};
}
function createBaseQueryEpochResponse() {
    return { epoch: undefined };
}
function isSet(value) {
    return value !== null && value !== undefined;
}
exports.registry = [
    ["/paxprotocol.paxchain.epoch.QueryParamsRequest", exports.QueryParamsRequest],
    ["/paxprotocol.paxchain.epoch.QueryParamsResponse", exports.QueryParamsResponse],
    ["/paxprotocol.paxchain.epoch.QueryEpochRequest", exports.QueryEpochRequest],
    ["/paxprotocol.paxchain.epoch.QueryEpochResponse", exports.QueryEpochResponse]
];
exports.aminoConverters = {
    "/paxprotocol.paxchain.epoch.QueryParamsRequest": {
        aminoType: "epoch/QueryParamsRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.epoch.QueryParamsResponse": {
        aminoType: "epoch/QueryParamsResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.epoch.QueryEpochRequest": {
        aminoType: "epoch/QueryEpochRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.epoch.QueryEpochResponse": {
        aminoType: "epoch/QueryEpochResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
