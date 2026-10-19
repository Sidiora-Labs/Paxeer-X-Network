"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.Pair = exports.Pairs = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
exports.Pairs = {
    $type: "cosmos.base.kv.v1beta1.Pairs",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.pairs) {
            exports.Pair.encode(v, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBasePairs();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.pairs.push(exports.Pair.decode(reader, reader.uint32()));
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
        return { pairs: globalThis.Array.isArray(object?.pairs) ? object.pairs.map((e) => exports.Pair.fromJSON(e)) : [] };
    },
    toJSON(message) {
        const obj = {};
        if (message.pairs?.length) {
            obj.pairs = message.pairs.map((e) => exports.Pair.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.Pairs.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBasePairs();
        message.pairs = object.pairs?.map((e) => exports.Pair.fromPartial(e)) || [];
        return message;
    }
};
exports.Pair = {
    $type: "cosmos.base.kv.v1beta1.Pair",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.key.length !== 0) {
            writer.uint32(10).bytes(message.key);
        }
        if (message.value.length !== 0) {
            writer.uint32(18).bytes(message.value);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBasePair();
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
                    if (tag !== 18) {
                        break;
                    }
                    message.value = reader.bytes();
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
            value: isSet(object.value) ? bytesFromBase64(object.value) : new Uint8Array(0)
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.key.length !== 0) {
            obj.key = base64FromBytes(message.key);
        }
        if (message.value.length !== 0) {
            obj.value = base64FromBytes(message.value);
        }
        return obj;
    },
    create(base) {
        return exports.Pair.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBasePair();
        message.key = object.key ?? new Uint8Array(0);
        message.value = object.value ?? new Uint8Array(0);
        return message;
    }
};
function createBasePairs() {
    return { pairs: [] };
}
function createBasePair() {
    return { key: new Uint8Array(0), value: new Uint8Array(0) };
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
    ["/cosmos.base.kv.v1beta1.Pairs", exports.Pairs],
    ["/cosmos.base.kv.v1beta1.Pair", exports.Pair]
];
exports.aminoConverters = {
    "/cosmos.base.kv.v1beta1.Pairs": {
        aminoType: "cosmos-sdk/Pairs",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.base.kv.v1beta1.Pair": {
        aminoType: "cosmos-sdk/Pair",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
