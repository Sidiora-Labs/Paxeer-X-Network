import { BinaryReader, BinaryWriter } from "@bufbuild/protobuf/wire";
export const MultiSignature = {
    $type: "cosmos.crypto.multisig.v1beta1.MultiSignature",
    encode(message, writer = new BinaryWriter()) {
        for (const v of message.signatures) {
            writer.uint32(10).bytes(v);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMultiSignature();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.signatures.push(reader.bytes());
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
            signatures: globalThis.Array.isArray(object?.signatures) ? object.signatures.map((e) => bytesFromBase64(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.signatures?.length) {
            obj.signatures = message.signatures.map((e) => base64FromBytes(e));
        }
        return obj;
    },
    create(base) {
        return MultiSignature.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMultiSignature();
        message.signatures = object.signatures?.map((e) => e) || [];
        return message;
    }
};
export const CompactBitArray = {
    $type: "cosmos.crypto.multisig.v1beta1.CompactBitArray",
    encode(message, writer = new BinaryWriter()) {
        if (message.extra_bits_stored !== 0) {
            writer.uint32(8).uint32(message.extra_bits_stored);
        }
        if (message.elems.length !== 0) {
            writer.uint32(18).bytes(message.elems);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseCompactBitArray();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.extra_bits_stored = reader.uint32();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.elems = reader.bytes();
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
            extra_bits_stored: isSet(object.extra_bits_stored) ? globalThis.Number(object.extra_bits_stored) : 0,
            elems: isSet(object.elems) ? bytesFromBase64(object.elems) : new Uint8Array(0)
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.extra_bits_stored !== 0) {
            obj.extra_bits_stored = Math.round(message.extra_bits_stored);
        }
        if (message.elems.length !== 0) {
            obj.elems = base64FromBytes(message.elems);
        }
        return obj;
    },
    create(base) {
        return CompactBitArray.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseCompactBitArray();
        message.extra_bits_stored = object.extra_bits_stored ?? 0;
        message.elems = object.elems ?? new Uint8Array(0);
        return message;
    }
};
function createBaseMultiSignature() {
    return { signatures: [] };
}
function createBaseCompactBitArray() {
    return { extra_bits_stored: 0, elems: new Uint8Array(0) };
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
export const registry = [
    ["/cosmos.crypto.multisig.v1beta1.MultiSignature", MultiSignature],
    ["/cosmos.crypto.multisig.v1beta1.CompactBitArray", CompactBitArray]
];
export const aminoConverters = {
    "/cosmos.crypto.multisig.v1beta1.MultiSignature": {
        aminoType: "cosmos-sdk/MultiSignature",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.crypto.multisig.v1beta1.CompactBitArray": {
        aminoType: "cosmos-sdk/CompactBitArray",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
