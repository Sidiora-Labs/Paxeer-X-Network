"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.PrivKey = exports.PubKey = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
exports.PubKey = {
    $type: "cosmos.crypto.secp256r1.PubKey",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.key.length !== 0) {
            writer.uint32(10).bytes(message.key);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBasePubKey();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.key = reader.bytes();
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
        return { key: isSet(object.key) ? bytesFromBase64(object.key) : new Uint8Array(0) };
    },
    toJSON(message) {
        const obj = {};
        if (message.key.length !== 0) {
            obj.key = base64FromBytes(message.key);
        }
        return obj;
    },
    create(base) {
        return exports.PubKey.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBasePubKey();
        message.key = object.key ?? new Uint8Array(0);
        return message;
    }
};
exports.PrivKey = {
    $type: "cosmos.crypto.secp256r1.PrivKey",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.secret.length !== 0) {
            writer.uint32(10).bytes(message.secret);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBasePrivKey();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.secret = reader.bytes();
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
        return { secret: isSet(object.secret) ? bytesFromBase64(object.secret) : new Uint8Array(0) };
    },
    toJSON(message) {
        const obj = {};
        if (message.secret.length !== 0) {
            obj.secret = base64FromBytes(message.secret);
        }
        return obj;
    },
    create(base) {
        return exports.PrivKey.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBasePrivKey();
        message.secret = object.secret ?? new Uint8Array(0);
        return message;
    }
};
function createBasePubKey() {
    return { key: new Uint8Array(0) };
}
function createBasePrivKey() {
    return { secret: new Uint8Array(0) };
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
    ["/cosmos.crypto.secp256r1.PubKey", exports.PubKey],
    ["/cosmos.crypto.secp256r1.PrivKey", exports.PrivKey]
];
exports.aminoConverters = {
    "/cosmos.crypto.secp256r1.PubKey": {
        aminoType: "cosmos-sdk/PubKey",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.crypto.secp256r1.PrivKey": {
        aminoType: "cosmos-sdk/PrivKey",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
