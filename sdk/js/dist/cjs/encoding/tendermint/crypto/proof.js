"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.ProofOps = exports.ProofOp = exports.DominoOp = exports.ValueOp = exports.Proof = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
exports.Proof = {
    $type: "tendermint.crypto.Proof",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.total !== 0) {
            writer.uint32(8).int64(message.total);
        }
        if (message.index !== 0) {
            writer.uint32(16).int64(message.index);
        }
        if (message.leaf_hash.length !== 0) {
            writer.uint32(26).bytes(message.leaf_hash);
        }
        for (const v of message.aunts) {
            writer.uint32(34).bytes(v);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseProof();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.total = longToNumber(reader.int64());
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.index = longToNumber(reader.int64());
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.leaf_hash = reader.bytes();
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.aunts.push(reader.bytes());
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
            total: isSet(object.total) ? globalThis.Number(object.total) : 0,
            index: isSet(object.index) ? globalThis.Number(object.index) : 0,
            leaf_hash: isSet(object.leaf_hash) ? bytesFromBase64(object.leaf_hash) : new Uint8Array(0),
            aunts: globalThis.Array.isArray(object?.aunts) ? object.aunts.map((e) => bytesFromBase64(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.total !== 0) {
            obj.total = Math.round(message.total);
        }
        if (message.index !== 0) {
            obj.index = Math.round(message.index);
        }
        if (message.leaf_hash.length !== 0) {
            obj.leaf_hash = base64FromBytes(message.leaf_hash);
        }
        if (message.aunts?.length) {
            obj.aunts = message.aunts.map((e) => base64FromBytes(e));
        }
        return obj;
    },
    create(base) {
        return exports.Proof.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseProof();
        message.total = object.total ?? 0;
        message.index = object.index ?? 0;
        message.leaf_hash = object.leaf_hash ?? new Uint8Array(0);
        message.aunts = object.aunts?.map((e) => e) || [];
        return message;
    }
};
exports.ValueOp = {
    $type: "tendermint.crypto.ValueOp",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.key.length !== 0) {
            writer.uint32(10).bytes(message.key);
        }
        if (message.proof !== undefined) {
            exports.Proof.encode(message.proof, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseValueOp();
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
                    message.proof = exports.Proof.decode(reader, reader.uint32());
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
            proof: isSet(object.proof) ? exports.Proof.fromJSON(object.proof) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.key.length !== 0) {
            obj.key = base64FromBytes(message.key);
        }
        if (message.proof !== undefined) {
            obj.proof = exports.Proof.toJSON(message.proof);
        }
        return obj;
    },
    create(base) {
        return exports.ValueOp.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseValueOp();
        message.key = object.key ?? new Uint8Array(0);
        message.proof = object.proof !== undefined && object.proof !== null ? exports.Proof.fromPartial(object.proof) : undefined;
        return message;
    }
};
exports.DominoOp = {
    $type: "tendermint.crypto.DominoOp",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.key !== "") {
            writer.uint32(10).string(message.key);
        }
        if (message.input !== "") {
            writer.uint32(18).string(message.input);
        }
        if (message.output !== "") {
            writer.uint32(26).string(message.output);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseDominoOp();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.key = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.input = reader.string();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.output = reader.string();
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
            key: isSet(object.key) ? globalThis.String(object.key) : "",
            input: isSet(object.input) ? globalThis.String(object.input) : "",
            output: isSet(object.output) ? globalThis.String(object.output) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.key !== "") {
            obj.key = message.key;
        }
        if (message.input !== "") {
            obj.input = message.input;
        }
        if (message.output !== "") {
            obj.output = message.output;
        }
        return obj;
    },
    create(base) {
        return exports.DominoOp.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseDominoOp();
        message.key = object.key ?? "";
        message.input = object.input ?? "";
        message.output = object.output ?? "";
        return message;
    }
};
exports.ProofOp = {
    $type: "tendermint.crypto.ProofOp",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.type !== "") {
            writer.uint32(10).string(message.type);
        }
        if (message.key.length !== 0) {
            writer.uint32(18).bytes(message.key);
        }
        if (message.data.length !== 0) {
            writer.uint32(26).bytes(message.data);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseProofOp();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.type = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.key = reader.bytes();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.data = reader.bytes();
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
            type: isSet(object.type) ? globalThis.String(object.type) : "",
            key: isSet(object.key) ? bytesFromBase64(object.key) : new Uint8Array(0),
            data: isSet(object.data) ? bytesFromBase64(object.data) : new Uint8Array(0)
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.type !== "") {
            obj.type = message.type;
        }
        if (message.key.length !== 0) {
            obj.key = base64FromBytes(message.key);
        }
        if (message.data.length !== 0) {
            obj.data = base64FromBytes(message.data);
        }
        return obj;
    },
    create(base) {
        return exports.ProofOp.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseProofOp();
        message.type = object.type ?? "";
        message.key = object.key ?? new Uint8Array(0);
        message.data = object.data ?? new Uint8Array(0);
        return message;
    }
};
exports.ProofOps = {
    $type: "tendermint.crypto.ProofOps",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.ops) {
            exports.ProofOp.encode(v, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseProofOps();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.ops.push(exports.ProofOp.decode(reader, reader.uint32()));
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
        return { ops: globalThis.Array.isArray(object?.ops) ? object.ops.map((e) => exports.ProofOp.fromJSON(e)) : [] };
    },
    toJSON(message) {
        const obj = {};
        if (message.ops?.length) {
            obj.ops = message.ops.map((e) => exports.ProofOp.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.ProofOps.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseProofOps();
        message.ops = object.ops?.map((e) => exports.ProofOp.fromPartial(e)) || [];
        return message;
    }
};
function createBaseProof() {
    return { total: 0, index: 0, leaf_hash: new Uint8Array(0), aunts: [] };
}
function createBaseValueOp() {
    return { key: new Uint8Array(0), proof: undefined };
}
function createBaseDominoOp() {
    return { key: "", input: "", output: "" };
}
function createBaseProofOp() {
    return { type: "", key: new Uint8Array(0), data: new Uint8Array(0) };
}
function createBaseProofOps() {
    return { ops: [] };
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
    ["/tendermint.crypto.Proof", exports.Proof],
    ["/tendermint.crypto.ValueOp", exports.ValueOp],
    ["/tendermint.crypto.DominoOp", exports.DominoOp],
    ["/tendermint.crypto.ProofOp", exports.ProofOp],
    ["/tendermint.crypto.ProofOps", exports.ProofOps]
];
exports.aminoConverters = {
    "/tendermint.crypto.Proof": {
        aminoType: "tendermint.crypto.Proof",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/tendermint.crypto.ValueOp": {
        aminoType: "tendermint.crypto.ValueOp",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/tendermint.crypto.DominoOp": {
        aminoType: "tendermint.crypto.DominoOp",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/tendermint.crypto.ProofOp": {
        aminoType: "tendermint.crypto.ProofOp",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/tendermint.crypto.ProofOps": {
        aminoType: "tendermint.crypto.ProofOps",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
