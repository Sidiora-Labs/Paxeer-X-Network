import { BinaryReader, BinaryWriter } from "@bufbuild/protobuf/wire";
export const PublicKey = {
    $type: "tendermint.crypto.PublicKey",
    encode(message, writer = new BinaryWriter()) {
        if (message.ed25519 !== undefined) {
            writer.uint32(10).bytes(message.ed25519);
        }
        if (message.secp256k1 !== undefined) {
            writer.uint32(18).bytes(message.secp256k1);
        }
        if (message.sr25519 !== undefined) {
            writer.uint32(26).bytes(message.sr25519);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBasePublicKey();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.ed25519 = reader.bytes();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.secp256k1 = reader.bytes();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.sr25519 = reader.bytes();
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
            ed25519: isSet(object.ed25519) ? bytesFromBase64(object.ed25519) : undefined,
            secp256k1: isSet(object.secp256k1) ? bytesFromBase64(object.secp256k1) : undefined,
            sr25519: isSet(object.sr25519) ? bytesFromBase64(object.sr25519) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.ed25519 !== undefined) {
            obj.ed25519 = base64FromBytes(message.ed25519);
        }
        if (message.secp256k1 !== undefined) {
            obj.secp256k1 = base64FromBytes(message.secp256k1);
        }
        if (message.sr25519 !== undefined) {
            obj.sr25519 = base64FromBytes(message.sr25519);
        }
        return obj;
    },
    create(base) {
        return PublicKey.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBasePublicKey();
        message.ed25519 = object.ed25519 ?? undefined;
        message.secp256k1 = object.secp256k1 ?? undefined;
        message.sr25519 = object.sr25519 ?? undefined;
        return message;
    }
};
function createBasePublicKey() {
    return { ed25519: undefined, secp256k1: undefined, sr25519: undefined };
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
export const registry = [["/tendermint.crypto.PublicKey", PublicKey]];
export const aminoConverters = {
    "/tendermint.crypto.PublicKey": {
        aminoType: "tendermint.crypto.PublicKey",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
