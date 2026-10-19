import { BinaryReader, BinaryWriter } from "@bufbuild/protobuf/wire";
import { Epoch } from "./epoch";
import { Params } from "./params";
export const QueryParamsRequest = {
    $type: "paxprotocol.paxchain.epoch.QueryParamsRequest",
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
    $type: "paxprotocol.paxchain.epoch.QueryParamsResponse",
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
export const QueryEpochRequest = {
    $type: "paxprotocol.paxchain.epoch.QueryEpochRequest",
    encode(_, writer = new BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
        return QueryEpochRequest.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseQueryEpochRequest();
        return message;
    }
};
export const QueryEpochResponse = {
    $type: "paxprotocol.paxchain.epoch.QueryEpochResponse",
    encode(message, writer = new BinaryWriter()) {
        if (message.epoch !== undefined) {
            Epoch.encode(message.epoch, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryEpochResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.epoch = Epoch.decode(reader, reader.uint32());
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
        return { epoch: isSet(object.epoch) ? Epoch.fromJSON(object.epoch) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.epoch !== undefined) {
            obj.epoch = Epoch.toJSON(message.epoch);
        }
        return obj;
    },
    create(base) {
        return QueryEpochResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryEpochResponse();
        message.epoch = object.epoch !== undefined && object.epoch !== null ? Epoch.fromPartial(object.epoch) : undefined;
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
export const registry = [
    ["/paxprotocol.paxchain.epoch.QueryParamsRequest", QueryParamsRequest],
    ["/paxprotocol.paxchain.epoch.QueryParamsResponse", QueryParamsResponse],
    ["/paxprotocol.paxchain.epoch.QueryEpochRequest", QueryEpochRequest],
    ["/paxprotocol.paxchain.epoch.QueryEpochResponse", QueryEpochResponse]
];
export const aminoConverters = {
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
