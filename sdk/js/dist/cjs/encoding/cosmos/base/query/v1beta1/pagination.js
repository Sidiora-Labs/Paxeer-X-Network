"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.PageResponse = exports.PageRequest = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
exports.PageRequest = {
    $type: "cosmos.base.query.v1beta1.PageRequest",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.key.length !== 0) {
            writer.uint32(10).bytes(message.key);
        }
        if (message.offset !== 0) {
            writer.uint32(16).uint64(message.offset);
        }
        if (message.limit !== 0) {
            writer.uint32(24).uint64(message.limit);
        }
        if (message.count_total !== false) {
            writer.uint32(32).bool(message.count_total);
        }
        if (message.reverse !== false) {
            writer.uint32(40).bool(message.reverse);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBasePageRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.key = reader.bytes();
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.offset = longToNumber(reader.uint64());
                    continue;
                case 3:
                    if (tag !== 24) {
                        break;
                    }
                    message.limit = longToNumber(reader.uint64());
                    continue;
                case 4:
                    if (tag !== 32) {
                        break;
                    }
                    message.count_total = reader.bool();
                    continue;
                case 5:
                    if (tag !== 40) {
                        break;
                    }
                    message.reverse = reader.bool();
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
            key: isSet(object.key) ? bytesFromBase64(object.key) : new Uint8Array(0),
            offset: isSet(object.offset) ? globalThis.Number(object.offset) : 0,
            limit: isSet(object.limit) ? globalThis.Number(object.limit) : 0,
            count_total: isSet(object.count_total) ? globalThis.Boolean(object.count_total) : false,
            reverse: isSet(object.reverse) ? globalThis.Boolean(object.reverse) : false
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.key.length !== 0) {
            obj.key = base64FromBytes(message.key);
        }
        if (message.offset !== 0) {
            obj.offset = Math.round(message.offset);
        }
        if (message.limit !== 0) {
            obj.limit = Math.round(message.limit);
        }
        if (message.count_total !== false) {
            obj.count_total = message.count_total;
        }
        if (message.reverse !== false) {
            obj.reverse = message.reverse;
        }
        return obj;
    },
    create(base) {
        return exports.PageRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBasePageRequest();
        message.key = object.key ?? new Uint8Array(0);
        message.offset = object.offset ?? 0;
        message.limit = object.limit ?? 0;
        message.count_total = object.count_total ?? false;
        message.reverse = object.reverse ?? false;
        return message;
    }
};
exports.PageResponse = {
    $type: "cosmos.base.query.v1beta1.PageResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.next_key.length !== 0) {
            writer.uint32(10).bytes(message.next_key);
        }
        if (message.total !== 0) {
            writer.uint32(16).uint64(message.total);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBasePageResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.next_key = reader.bytes();
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.total = longToNumber(reader.uint64());
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
            next_key: isSet(object.next_key) ? bytesFromBase64(object.next_key) : new Uint8Array(0),
            total: isSet(object.total) ? globalThis.Number(object.total) : 0
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.next_key.length !== 0) {
            obj.next_key = base64FromBytes(message.next_key);
        }
        if (message.total !== 0) {
            obj.total = Math.round(message.total);
        }
        return obj;
    },
    create(base) {
        return exports.PageResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBasePageResponse();
        message.next_key = object.next_key ?? new Uint8Array(0);
        message.total = object.total ?? 0;
        return message;
    }
};
function createBasePageRequest() {
    return { key: new Uint8Array(0), offset: 0, limit: 0, count_total: false, reverse: false };
}
function createBasePageResponse() {
    return { next_key: new Uint8Array(0), total: 0 };
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
exports.registry = [
    ["/cosmos.base.query.v1beta1.PageRequest", exports.PageRequest],
    ["/cosmos.base.query.v1beta1.PageResponse", exports.PageResponse]
];
exports.aminoConverters = {
    "/cosmos.base.query.v1beta1.PageRequest": {
        aminoType: "cosmos-sdk/PageRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.base.query.v1beta1.PageResponse": {
        aminoType: "cosmos-sdk/PageResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
