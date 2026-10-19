"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.SignatureDescriptorDataMulti = exports.SignatureDescriptorDataSingle = exports.SignatureDescriptorData = exports.SignatureDescriptor = exports.SignatureDescriptors = void 0;
exports.signModeFromJSON = signModeFromJSON;
exports.signModeToJSON = signModeToJSON;
const wire_1 = require("@bufbuild/protobuf/wire");
const any_1 = require("../../../../google/protobuf/any");
const multisig_1 = require("../../../crypto/multisig/v1beta1/multisig");
const v1beta1_1 = require("../../../../../types/cosmos/tx/signing/v1beta1");
exports.SignatureDescriptors = {
    $type: "cosmos.tx.signing.v1beta1.SignatureDescriptors",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.signatures) {
            exports.SignatureDescriptor.encode(v, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseSignatureDescriptors();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.signatures.push(exports.SignatureDescriptor.decode(reader, reader.uint32()));
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
            signatures: globalThis.Array.isArray(object?.signatures) ? object.signatures.map((e) => exports.SignatureDescriptor.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.signatures?.length) {
            obj.signatures = message.signatures.map((e) => exports.SignatureDescriptor.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.SignatureDescriptors.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseSignatureDescriptors();
        message.signatures = object.signatures?.map((e) => exports.SignatureDescriptor.fromPartial(e)) || [];
        return message;
    }
};
exports.SignatureDescriptor = {
    $type: "cosmos.tx.signing.v1beta1.SignatureDescriptor",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.public_key !== undefined) {
            any_1.Any.encode(message.public_key, writer.uint32(10).fork()).join();
        }
        if (message.data !== undefined) {
            exports.SignatureDescriptorData.encode(message.data, writer.uint32(18).fork()).join();
        }
        if (message.sequence !== 0) {
            writer.uint32(24).uint64(message.sequence);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseSignatureDescriptor();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.public_key = any_1.Any.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.data = exports.SignatureDescriptorData.decode(reader, reader.uint32());
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
            public_key: isSet(object.public_key) ? any_1.Any.fromJSON(object.public_key) : undefined,
            data: isSet(object.data) ? exports.SignatureDescriptorData.fromJSON(object.data) : undefined,
            sequence: isSet(object.sequence) ? globalThis.Number(object.sequence) : 0
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.public_key !== undefined) {
            obj.public_key = any_1.Any.toJSON(message.public_key);
        }
        if (message.data !== undefined) {
            obj.data = exports.SignatureDescriptorData.toJSON(message.data);
        }
        if (message.sequence !== 0) {
            obj.sequence = Math.round(message.sequence);
        }
        return obj;
    },
    create(base) {
        return exports.SignatureDescriptor.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseSignatureDescriptor();
        message.public_key = object.public_key !== undefined && object.public_key !== null ? any_1.Any.fromPartial(object.public_key) : undefined;
        message.data = object.data !== undefined && object.data !== null ? exports.SignatureDescriptorData.fromPartial(object.data) : undefined;
        message.sequence = object.sequence ?? 0;
        return message;
    }
};
exports.SignatureDescriptorData = {
    $type: "cosmos.tx.signing.v1beta1.SignatureDescriptor.Data",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.single !== undefined) {
            exports.SignatureDescriptorDataSingle.encode(message.single, writer.uint32(10).fork()).join();
        }
        if (message.multi !== undefined) {
            exports.SignatureDescriptorDataMulti.encode(message.multi, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseSignatureDescriptorData();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.single = exports.SignatureDescriptorDataSingle.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.multi = exports.SignatureDescriptorDataMulti.decode(reader, reader.uint32());
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
            single: isSet(object.single) ? exports.SignatureDescriptorDataSingle.fromJSON(object.single) : undefined,
            multi: isSet(object.multi) ? exports.SignatureDescriptorDataMulti.fromJSON(object.multi) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.single !== undefined) {
            obj.single = exports.SignatureDescriptorDataSingle.toJSON(message.single);
        }
        if (message.multi !== undefined) {
            obj.multi = exports.SignatureDescriptorDataMulti.toJSON(message.multi);
        }
        return obj;
    },
    create(base) {
        return exports.SignatureDescriptorData.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseSignatureDescriptorData();
        message.single = object.single !== undefined && object.single !== null ? exports.SignatureDescriptorDataSingle.fromPartial(object.single) : undefined;
        message.multi = object.multi !== undefined && object.multi !== null ? exports.SignatureDescriptorDataMulti.fromPartial(object.multi) : undefined;
        return message;
    }
};
exports.SignatureDescriptorDataSingle = {
    $type: "cosmos.tx.signing.v1beta1.SignatureDescriptor.Data.Single",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.mode !== 0) {
            writer.uint32(8).int32(message.mode);
        }
        if (message.signature.length !== 0) {
            writer.uint32(18).bytes(message.signature);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
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
        return exports.SignatureDescriptorDataSingle.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseSignatureDescriptorDataSingle();
        message.mode = object.mode ?? 0;
        message.signature = object.signature ?? new Uint8Array(0);
        return message;
    }
};
exports.SignatureDescriptorDataMulti = {
    $type: "cosmos.tx.signing.v1beta1.SignatureDescriptor.Data.Multi",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.bitarray !== undefined) {
            multisig_1.CompactBitArray.encode(message.bitarray, writer.uint32(10).fork()).join();
        }
        for (const v of message.signatures) {
            exports.SignatureDescriptorData.encode(v, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseSignatureDescriptorDataMulti();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.bitarray = multisig_1.CompactBitArray.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.signatures.push(exports.SignatureDescriptorData.decode(reader, reader.uint32()));
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
            bitarray: isSet(object.bitarray) ? multisig_1.CompactBitArray.fromJSON(object.bitarray) : undefined,
            signatures: globalThis.Array.isArray(object?.signatures) ? object.signatures.map((e) => exports.SignatureDescriptorData.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.bitarray !== undefined) {
            obj.bitarray = multisig_1.CompactBitArray.toJSON(message.bitarray);
        }
        if (message.signatures?.length) {
            obj.signatures = message.signatures.map((e) => exports.SignatureDescriptorData.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.SignatureDescriptorDataMulti.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseSignatureDescriptorDataMulti();
        message.bitarray = object.bitarray !== undefined && object.bitarray !== null ? multisig_1.CompactBitArray.fromPartial(object.bitarray) : undefined;
        message.signatures = object.signatures?.map((e) => exports.SignatureDescriptorData.fromPartial(e)) || [];
        return message;
    }
};
function signModeFromJSON(object) {
    switch (object) {
        case 0:
        case "SIGN_MODE_UNSPECIFIED":
            return v1beta1_1.SignMode.SIGN_MODE_UNSPECIFIED;
        case 1:
        case "SIGN_MODE_DIRECT":
            return v1beta1_1.SignMode.SIGN_MODE_DIRECT;
        case 2:
        case "SIGN_MODE_TEXTUAL":
            return v1beta1_1.SignMode.SIGN_MODE_TEXTUAL;
        case 127:
        case "SIGN_MODE_LEGACY_AMINO_JSON":
            return v1beta1_1.SignMode.SIGN_MODE_LEGACY_AMINO_JSON;
        case 191:
        case "SIGN_MODE_EIP_191":
            return v1beta1_1.SignMode.SIGN_MODE_EIP_191;
        case -1:
        case "UNRECOGNIZED":
        default:
            return v1beta1_1.SignMode.UNRECOGNIZED;
    }
}
function signModeToJSON(object) {
    switch (object) {
        case v1beta1_1.SignMode.SIGN_MODE_UNSPECIFIED:
            return "SIGN_MODE_UNSPECIFIED";
        case v1beta1_1.SignMode.SIGN_MODE_DIRECT:
            return "SIGN_MODE_DIRECT";
        case v1beta1_1.SignMode.SIGN_MODE_TEXTUAL:
            return "SIGN_MODE_TEXTUAL";
        case v1beta1_1.SignMode.SIGN_MODE_LEGACY_AMINO_JSON:
            return "SIGN_MODE_LEGACY_AMINO_JSON";
        case v1beta1_1.SignMode.SIGN_MODE_EIP_191:
            return "SIGN_MODE_EIP_191";
        case v1beta1_1.SignMode.UNRECOGNIZED:
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
exports.registry = [
    ["/cosmos.tx.signing.v1beta1.SignatureDescriptors", exports.SignatureDescriptors],
    ["/cosmos.tx.signing.v1beta1.SignatureDescriptor", exports.SignatureDescriptor],
    ["/cosmos.tx.signing.v1beta1.SignatureDescriptor.Data", exports.SignatureDescriptorData],
    ["/cosmos.tx.signing.v1beta1.SignatureDescriptor.Data.Single", exports.SignatureDescriptorDataSingle],
    ["/cosmos.tx.signing.v1beta1.SignatureDescriptor.Data.Multi", exports.SignatureDescriptorDataMulti]
];
exports.aminoConverters = {
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
