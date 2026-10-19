import { BinaryReader, BinaryWriter } from "@bufbuild/protobuf/wire";
import { Any } from "../../../../google/protobuf/any";
import { CompactBitArray } from "../../../crypto/multisig/v1beta1/multisig";
import { SignMode } from "../../../../../types/cosmos/tx/signing/v1beta1";
export const SignatureDescriptors = {
    $type: "cosmos.tx.signing.v1beta1.SignatureDescriptors",
    encode(message, writer = new BinaryWriter()) {
        for (const v of message.signatures) {
            SignatureDescriptor.encode(v, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseSignatureDescriptors();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.signatures.push(SignatureDescriptor.decode(reader, reader.uint32()));
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
            signatures: globalThis.Array.isArray(object?.signatures) ? object.signatures.map((e) => SignatureDescriptor.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.signatures?.length) {
            obj.signatures = message.signatures.map((e) => SignatureDescriptor.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return SignatureDescriptors.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseSignatureDescriptors();
        message.signatures = object.signatures?.map((e) => SignatureDescriptor.fromPartial(e)) || [];
        return message;
    }
};
export const SignatureDescriptor = {
    $type: "cosmos.tx.signing.v1beta1.SignatureDescriptor",
    encode(message, writer = new BinaryWriter()) {
        if (message.public_key !== undefined) {
            Any.encode(message.public_key, writer.uint32(10).fork()).join();
        }
        if (message.data !== undefined) {
            SignatureDescriptorData.encode(message.data, writer.uint32(18).fork()).join();
        }
        if (message.sequence !== 0) {
            writer.uint32(24).uint64(message.sequence);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseSignatureDescriptor();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.public_key = Any.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.data = SignatureDescriptorData.decode(reader, reader.uint32());
                    continue;
                case 3:
                    if (tag !== 24) {
                        break;
                    }
                    message.sequence = longToNumber(reader.uint64());
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
            public_key: isSet(object.public_key) ? Any.fromJSON(object.public_key) : undefined,
            data: isSet(object.data) ? SignatureDescriptorData.fromJSON(object.data) : undefined,
            sequence: isSet(object.sequence) ? globalThis.Number(object.sequence) : 0
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.public_key !== undefined) {
            obj.public_key = Any.toJSON(message.public_key);
        }
        if (message.data !== undefined) {
            obj.data = SignatureDescriptorData.toJSON(message.data);
        }
        if (message.sequence !== 0) {
            obj.sequence = Math.round(message.sequence);
        }
        return obj;
    },
    create(base) {
        return SignatureDescriptor.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseSignatureDescriptor();
        message.public_key = object.public_key !== undefined && object.public_key !== null ? Any.fromPartial(object.public_key) : undefined;
        message.data = object.data !== undefined && object.data !== null ? SignatureDescriptorData.fromPartial(object.data) : undefined;
        message.sequence = object.sequence ?? 0;
        return message;
    }
};
export const SignatureDescriptorData = {
    $type: "cosmos.tx.signing.v1beta1.SignatureDescriptor.Data",
    encode(message, writer = new BinaryWriter()) {
        if (message.single !== undefined) {
            SignatureDescriptorDataSingle.encode(message.single, writer.uint32(10).fork()).join();
        }
        if (message.multi !== undefined) {
            SignatureDescriptorDataMulti.encode(message.multi, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseSignatureDescriptorData();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.single = SignatureDescriptorDataSingle.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.multi = SignatureDescriptorDataMulti.decode(reader, reader.uint32());
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
            single: isSet(object.single) ? SignatureDescriptorDataSingle.fromJSON(object.single) : undefined,
            multi: isSet(object.multi) ? SignatureDescriptorDataMulti.fromJSON(object.multi) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.single !== undefined) {
            obj.single = SignatureDescriptorDataSingle.toJSON(message.single);
        }
        if (message.multi !== undefined) {
            obj.multi = SignatureDescriptorDataMulti.toJSON(message.multi);
        }
        return obj;
    },
    create(base) {
        return SignatureDescriptorData.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseSignatureDescriptorData();
        message.single = object.single !== undefined && object.single !== null ? SignatureDescriptorDataSingle.fromPartial(object.single) : undefined;
        message.multi = object.multi !== undefined && object.multi !== null ? SignatureDescriptorDataMulti.fromPartial(object.multi) : undefined;
        return message;
    }
};
export const SignatureDescriptorDataSingle = {
    $type: "cosmos.tx.signing.v1beta1.SignatureDescriptor.Data.Single",
    encode(message, writer = new BinaryWriter()) {
        if (message.mode !== 0) {
            writer.uint32(8).int32(message.mode);
        }
        if (message.signature.length !== 0) {
            writer.uint32(18).bytes(message.signature);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseSignatureDescriptorDataSingle();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.mode = reader.int32();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.signature = reader.bytes();
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
            mode: isSet(object.mode) ? signModeFromJSON(object.mode) : 0,
            signature: isSet(object.signature) ? bytesFromBase64(object.signature) : new Uint8Array(0)
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.mode !== 0) {
            obj.mode = signModeToJSON(message.mode);
        }
        if (message.signature.length !== 0) {
            obj.signature = base64FromBytes(message.signature);
        }
        return obj;
    },
    create(base) {
        return SignatureDescriptorDataSingle.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseSignatureDescriptorDataSingle();
        message.mode = object.mode ?? 0;
        message.signature = object.signature ?? new Uint8Array(0);
        return message;
    }
};
export const SignatureDescriptorDataMulti = {
    $type: "cosmos.tx.signing.v1beta1.SignatureDescriptor.Data.Multi",
    encode(message, writer = new BinaryWriter()) {
        if (message.bitarray !== undefined) {
            CompactBitArray.encode(message.bitarray, writer.uint32(10).fork()).join();
        }
        for (const v of message.signatures) {
            SignatureDescriptorData.encode(v, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseSignatureDescriptorDataMulti();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.bitarray = CompactBitArray.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.signatures.push(SignatureDescriptorData.decode(reader, reader.uint32()));
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
            bitarray: isSet(object.bitarray) ? CompactBitArray.fromJSON(object.bitarray) : undefined,
            signatures: globalThis.Array.isArray(object?.signatures) ? object.signatures.map((e) => SignatureDescriptorData.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.bitarray !== undefined) {
            obj.bitarray = CompactBitArray.toJSON(message.bitarray);
        }
        if (message.signatures?.length) {
            obj.signatures = message.signatures.map((e) => SignatureDescriptorData.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return SignatureDescriptorDataMulti.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseSignatureDescriptorDataMulti();
        message.bitarray = object.bitarray !== undefined && object.bitarray !== null ? CompactBitArray.fromPartial(object.bitarray) : undefined;
        message.signatures = object.signatures?.map((e) => SignatureDescriptorData.fromPartial(e)) || [];
        return message;
    }
};
export function signModeFromJSON(object) {
    switch (object) {
        case 0:
        case "SIGN_MODE_UNSPECIFIED":
            return SignMode.SIGN_MODE_UNSPECIFIED;
        case 1:
        case "SIGN_MODE_DIRECT":
            return SignMode.SIGN_MODE_DIRECT;
        case 2:
        case "SIGN_MODE_TEXTUAL":
            return SignMode.SIGN_MODE_TEXTUAL;
        case 127:
        case "SIGN_MODE_LEGACY_AMINO_JSON":
            return SignMode.SIGN_MODE_LEGACY_AMINO_JSON;
        case 191:
        case "SIGN_MODE_EIP_191":
            return SignMode.SIGN_MODE_EIP_191;
        case -1:
        case "UNRECOGNIZED":
        default:
            return SignMode.UNRECOGNIZED;
    }
}
export function signModeToJSON(object) {
    switch (object) {
        case SignMode.SIGN_MODE_UNSPECIFIED:
            return "SIGN_MODE_UNSPECIFIED";
        case SignMode.SIGN_MODE_DIRECT:
            return "SIGN_MODE_DIRECT";
        case SignMode.SIGN_MODE_TEXTUAL:
            return "SIGN_MODE_TEXTUAL";
        case SignMode.SIGN_MODE_LEGACY_AMINO_JSON:
            return "SIGN_MODE_LEGACY_AMINO_JSON";
        case SignMode.SIGN_MODE_EIP_191:
            return "SIGN_MODE_EIP_191";
        case SignMode.UNRECOGNIZED:
        default:
            return "UNRECOGNIZED";
    }
}
function createBaseSignatureDescriptors() {
    return { signatures: [] };
}
function createBaseSignatureDescriptor() {
    return { public_key: undefined, data: undefined, sequence: 0 };
}
function createBaseSignatureDescriptorData() {
    return { single: undefined, multi: undefined };
}
function createBaseSignatureDescriptorDataSingle() {
    return { mode: 0, signature: new Uint8Array(0) };
}
function createBaseSignatureDescriptorDataMulti() {
    return { bitarray: undefined, signatures: [] };
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
export const registry = [
    ["/cosmos.tx.signing.v1beta1.SignatureDescriptors", SignatureDescriptors],
    ["/cosmos.tx.signing.v1beta1.SignatureDescriptor", SignatureDescriptor],
    ["/cosmos.tx.signing.v1beta1.SignatureDescriptor.Data", SignatureDescriptorData],
    ["/cosmos.tx.signing.v1beta1.SignatureDescriptor.Data.Single", SignatureDescriptorDataSingle],
    ["/cosmos.tx.signing.v1beta1.SignatureDescriptor.Data.Multi", SignatureDescriptorDataMulti]
];
export const aminoConverters = {
    "/cosmos.tx.signing.v1beta1.SignatureDescriptors": {
        aminoType: "cosmos-sdk/SignatureDescriptors",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.tx.signing.v1beta1.SignatureDescriptor": {
        aminoType: "cosmos-sdk/SignatureDescriptor",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.tx.signing.v1beta1.SignatureDescriptor.Data": {
        aminoType: "cosmos-sdk/Data",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.tx.signing.v1beta1.SignatureDescriptor.Data.Single": {
        aminoType: "cosmos-sdk/Single",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.tx.signing.v1beta1.SignatureDescriptor.Data.Multi": {
        aminoType: "cosmos-sdk/Multi",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
