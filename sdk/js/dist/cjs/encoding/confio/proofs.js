"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.CompressedNonExistenceProof = exports.CompressedExistenceProof = exports.CompressedBatchEntry = exports.CompressedBatchProof = exports.BatchEntry = exports.BatchProof = exports.InnerSpec = exports.ProofSpec = exports.InnerOp = exports.LeafOp = exports.CommitmentProof = exports.NonExistenceProof = exports.ExistenceProof = void 0;
exports.hashOpFromJSON = hashOpFromJSON;
exports.hashOpToJSON = hashOpToJSON;
exports.lengthOpFromJSON = lengthOpFromJSON;
exports.lengthOpToJSON = lengthOpToJSON;
const wire_1 = require("@bufbuild/protobuf/wire");
const confio_1 = require("../../types/confio");
exports.ExistenceProof = {
    $type: "ics23.ExistenceProof",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.key.length !== 0) {
            writer.uint32(10).bytes(message.key);
        }
        if (message.value.length !== 0) {
            writer.uint32(18).bytes(message.value);
        }
        if (message.leaf !== undefined) {
            exports.LeafOp.encode(message.leaf, writer.uint32(26).fork()).join();
        }
        for (const v of message.path) {
            exports.InnerOp.encode(v, writer.uint32(34).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseExistenceProof();
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
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.leaf = exports.LeafOp.decode(reader, reader.uint32());
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.path.push(exports.InnerOp.decode(reader, reader.uint32()));
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
            value: isSet(object.value) ? bytesFromBase64(object.value) : new Uint8Array(0),
            leaf: isSet(object.leaf) ? exports.LeafOp.fromJSON(object.leaf) : undefined,
            path: globalThis.Array.isArray(object?.path) ? object.path.map((e) => exports.InnerOp.fromJSON(e)) : []
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
        if (message.leaf !== undefined) {
            obj.leaf = exports.LeafOp.toJSON(message.leaf);
        }
        if (message.path?.length) {
            obj.path = message.path.map((e) => exports.InnerOp.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.ExistenceProof.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseExistenceProof();
        message.key = object.key ?? new Uint8Array(0);
        message.value = object.value ?? new Uint8Array(0);
        message.leaf = object.leaf !== undefined && object.leaf !== null ? exports.LeafOp.fromPartial(object.leaf) : undefined;
        message.path = object.path?.map((e) => exports.InnerOp.fromPartial(e)) || [];
        return message;
    }
};
exports.NonExistenceProof = {
    $type: "ics23.NonExistenceProof",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.key.length !== 0) {
            writer.uint32(10).bytes(message.key);
        }
        if (message.left !== undefined) {
            exports.ExistenceProof.encode(message.left, writer.uint32(18).fork()).join();
        }
        if (message.right !== undefined) {
            exports.ExistenceProof.encode(message.right, writer.uint32(26).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseNonExistenceProof();
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
                    message.left = exports.ExistenceProof.decode(reader, reader.uint32());
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.right = exports.ExistenceProof.decode(reader, reader.uint32());
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
            left: isSet(object.left) ? exports.ExistenceProof.fromJSON(object.left) : undefined,
            right: isSet(object.right) ? exports.ExistenceProof.fromJSON(object.right) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.key.length !== 0) {
            obj.key = base64FromBytes(message.key);
        }
        if (message.left !== undefined) {
            obj.left = exports.ExistenceProof.toJSON(message.left);
        }
        if (message.right !== undefined) {
            obj.right = exports.ExistenceProof.toJSON(message.right);
        }
        return obj;
    },
    create(base) {
        return exports.NonExistenceProof.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseNonExistenceProof();
        message.key = object.key ?? new Uint8Array(0);
        message.left = object.left !== undefined && object.left !== null ? exports.ExistenceProof.fromPartial(object.left) : undefined;
        message.right = object.right !== undefined && object.right !== null ? exports.ExistenceProof.fromPartial(object.right) : undefined;
        return message;
    }
};
exports.CommitmentProof = {
    $type: "ics23.CommitmentProof",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.exist !== undefined) {
            exports.ExistenceProof.encode(message.exist, writer.uint32(10).fork()).join();
        }
        if (message.nonexist !== undefined) {
            exports.NonExistenceProof.encode(message.nonexist, writer.uint32(18).fork()).join();
        }
        if (message.batch !== undefined) {
            exports.BatchProof.encode(message.batch, writer.uint32(26).fork()).join();
        }
        if (message.compressed !== undefined) {
            exports.CompressedBatchProof.encode(message.compressed, writer.uint32(34).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseCommitmentProof();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.exist = exports.ExistenceProof.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.nonexist = exports.NonExistenceProof.decode(reader, reader.uint32());
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.batch = exports.BatchProof.decode(reader, reader.uint32());
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.compressed = exports.CompressedBatchProof.decode(reader, reader.uint32());
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
            exist: isSet(object.exist) ? exports.ExistenceProof.fromJSON(object.exist) : undefined,
            nonexist: isSet(object.nonexist) ? exports.NonExistenceProof.fromJSON(object.nonexist) : undefined,
            batch: isSet(object.batch) ? exports.BatchProof.fromJSON(object.batch) : undefined,
            compressed: isSet(object.compressed) ? exports.CompressedBatchProof.fromJSON(object.compressed) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.exist !== undefined) {
            obj.exist = exports.ExistenceProof.toJSON(message.exist);
        }
        if (message.nonexist !== undefined) {
            obj.nonexist = exports.NonExistenceProof.toJSON(message.nonexist);
        }
        if (message.batch !== undefined) {
            obj.batch = exports.BatchProof.toJSON(message.batch);
        }
        if (message.compressed !== undefined) {
            obj.compressed = exports.CompressedBatchProof.toJSON(message.compressed);
        }
        return obj;
    },
    create(base) {
        return exports.CommitmentProof.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseCommitmentProof();
        message.exist = object.exist !== undefined && object.exist !== null ? exports.ExistenceProof.fromPartial(object.exist) : undefined;
        message.nonexist = object.nonexist !== undefined && object.nonexist !== null ? exports.NonExistenceProof.fromPartial(object.nonexist) : undefined;
        message.batch = object.batch !== undefined && object.batch !== null ? exports.BatchProof.fromPartial(object.batch) : undefined;
        message.compressed = object.compressed !== undefined && object.compressed !== null ? exports.CompressedBatchProof.fromPartial(object.compressed) : undefined;
        return message;
    }
};
exports.LeafOp = {
    $type: "ics23.LeafOp",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.hash !== 0) {
            writer.uint32(8).int32(message.hash);
        }
        if (message.prehash_key !== 0) {
            writer.uint32(16).int32(message.prehash_key);
        }
        if (message.prehash_value !== 0) {
            writer.uint32(24).int32(message.prehash_value);
        }
        if (message.length !== 0) {
            writer.uint32(32).int32(message.length);
        }
        if (message.prefix.length !== 0) {
            writer.uint32(42).bytes(message.prefix);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseLeafOp();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.hash = reader.int32();
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.prehash_key = reader.int32();
                    continue;
                case 3:
                    if (tag !== 24) {
                        break;
                    }
                    message.prehash_value = reader.int32();
                    continue;
                case 4:
                    if (tag !== 32) {
                        break;
                    }
                    message.length = reader.int32();
                    continue;
                case 5:
                    if (tag !== 42) {
                        break;
                    }
                    message.prefix = reader.bytes();
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
            hash: isSet(object.hash) ? hashOpFromJSON(object.hash) : 0,
            prehash_key: isSet(object.prehash_key) ? hashOpFromJSON(object.prehash_key) : 0,
            prehash_value: isSet(object.prehash_value) ? hashOpFromJSON(object.prehash_value) : 0,
            length: isSet(object.length) ? lengthOpFromJSON(object.length) : 0,
            prefix: isSet(object.prefix) ? bytesFromBase64(object.prefix) : new Uint8Array(0)
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.hash !== 0) {
            obj.hash = hashOpToJSON(message.hash);
        }
        if (message.prehash_key !== 0) {
            obj.prehash_key = hashOpToJSON(message.prehash_key);
        }
        if (message.prehash_value !== 0) {
            obj.prehash_value = hashOpToJSON(message.prehash_value);
        }
        if (message.length !== 0) {
            obj.length = lengthOpToJSON(message.length);
        }
        if (message.prefix.length !== 0) {
            obj.prefix = base64FromBytes(message.prefix);
        }
        return obj;
    },
    create(base) {
        return exports.LeafOp.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseLeafOp();
        message.hash = object.hash ?? 0;
        message.prehash_key = object.prehash_key ?? 0;
        message.prehash_value = object.prehash_value ?? 0;
        message.length = object.length ?? 0;
        message.prefix = object.prefix ?? new Uint8Array(0);
        return message;
    }
};
exports.InnerOp = {
    $type: "ics23.InnerOp",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.hash !== 0) {
            writer.uint32(8).int32(message.hash);
        }
        if (message.prefix.length !== 0) {
            writer.uint32(18).bytes(message.prefix);
        }
        if (message.suffix.length !== 0) {
            writer.uint32(26).bytes(message.suffix);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseInnerOp();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.hash = reader.int32();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.prefix = reader.bytes();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.suffix = reader.bytes();
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
            hash: isSet(object.hash) ? hashOpFromJSON(object.hash) : 0,
            prefix: isSet(object.prefix) ? bytesFromBase64(object.prefix) : new Uint8Array(0),
            suffix: isSet(object.suffix) ? bytesFromBase64(object.suffix) : new Uint8Array(0)
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.hash !== 0) {
            obj.hash = hashOpToJSON(message.hash);
        }
        if (message.prefix.length !== 0) {
            obj.prefix = base64FromBytes(message.prefix);
        }
        if (message.suffix.length !== 0) {
            obj.suffix = base64FromBytes(message.suffix);
        }
        return obj;
    },
    create(base) {
        return exports.InnerOp.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseInnerOp();
        message.hash = object.hash ?? 0;
        message.prefix = object.prefix ?? new Uint8Array(0);
        message.suffix = object.suffix ?? new Uint8Array(0);
        return message;
    }
};
exports.ProofSpec = {
    $type: "ics23.ProofSpec",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.leaf_spec !== undefined) {
            exports.LeafOp.encode(message.leaf_spec, writer.uint32(10).fork()).join();
        }
        if (message.inner_spec !== undefined) {
            exports.InnerSpec.encode(message.inner_spec, writer.uint32(18).fork()).join();
        }
        if (message.max_depth !== 0) {
            writer.uint32(24).int32(message.max_depth);
        }
        if (message.min_depth !== 0) {
            writer.uint32(32).int32(message.min_depth);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseProofSpec();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.leaf_spec = exports.LeafOp.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.inner_spec = exports.InnerSpec.decode(reader, reader.uint32());
                    continue;
                case 3:
                    if (tag !== 24) {
                        break;
                    }
                    message.max_depth = reader.int32();
                    continue;
                case 4:
                    if (tag !== 32) {
                        break;
                    }
                    message.min_depth = reader.int32();
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
            leaf_spec: isSet(object.leaf_spec) ? exports.LeafOp.fromJSON(object.leaf_spec) : undefined,
            inner_spec: isSet(object.inner_spec) ? exports.InnerSpec.fromJSON(object.inner_spec) : undefined,
            max_depth: isSet(object.max_depth) ? globalThis.Number(object.max_depth) : 0,
            min_depth: isSet(object.min_depth) ? globalThis.Number(object.min_depth) : 0
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.leaf_spec !== undefined) {
            obj.leaf_spec = exports.LeafOp.toJSON(message.leaf_spec);
        }
        if (message.inner_spec !== undefined) {
            obj.inner_spec = exports.InnerSpec.toJSON(message.inner_spec);
        }
        if (message.max_depth !== 0) {
            obj.max_depth = Math.round(message.max_depth);
        }
        if (message.min_depth !== 0) {
            obj.min_depth = Math.round(message.min_depth);
        }
        return obj;
    },
    create(base) {
        return exports.ProofSpec.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseProofSpec();
        message.leaf_spec = object.leaf_spec !== undefined && object.leaf_spec !== null ? exports.LeafOp.fromPartial(object.leaf_spec) : undefined;
        message.inner_spec = object.inner_spec !== undefined && object.inner_spec !== null ? exports.InnerSpec.fromPartial(object.inner_spec) : undefined;
        message.max_depth = object.max_depth ?? 0;
        message.min_depth = object.min_depth ?? 0;
        return message;
    }
};
exports.InnerSpec = {
    $type: "ics23.InnerSpec",
    encode(message, writer = new wire_1.BinaryWriter()) {
        writer.uint32(10).fork();
        for (const v of message.child_order) {
            writer.int32(v);
        }
        writer.join();
        if (message.child_size !== 0) {
            writer.uint32(16).int32(message.child_size);
        }
        if (message.min_prefix_length !== 0) {
            writer.uint32(24).int32(message.min_prefix_length);
        }
        if (message.max_prefix_length !== 0) {
            writer.uint32(32).int32(message.max_prefix_length);
        }
        if (message.empty_child.length !== 0) {
            writer.uint32(42).bytes(message.empty_child);
        }
        if (message.hash !== 0) {
            writer.uint32(48).int32(message.hash);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseInnerSpec();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag === 8) {
                        message.child_order.push(reader.int32());
                        continue;
                    }
                    if (tag === 10) {
                        const end2 = reader.uint32() + reader.pos;
                        while (reader.pos < end2) {
                            message.child_order.push(reader.int32());
                        }
                        continue;
                    }
                    break;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.child_size = reader.int32();
                    continue;
                case 3:
                    if (tag !== 24) {
                        break;
                    }
                    message.min_prefix_length = reader.int32();
                    continue;
                case 4:
                    if (tag !== 32) {
                        break;
                    }
                    message.max_prefix_length = reader.int32();
                    continue;
                case 5:
                    if (tag !== 42) {
                        break;
                    }
                    message.empty_child = reader.bytes();
                    continue;
                case 6:
                    if (tag !== 48) {
                        break;
                    }
                    message.hash = reader.int32();
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
            child_order: globalThis.Array.isArray(object?.child_order) ? object.child_order.map((e) => globalThis.Number(e)) : [],
            child_size: isSet(object.child_size) ? globalThis.Number(object.child_size) : 0,
            min_prefix_length: isSet(object.min_prefix_length) ? globalThis.Number(object.min_prefix_length) : 0,
            max_prefix_length: isSet(object.max_prefix_length) ? globalThis.Number(object.max_prefix_length) : 0,
            empty_child: isSet(object.empty_child) ? bytesFromBase64(object.empty_child) : new Uint8Array(0),
            hash: isSet(object.hash) ? hashOpFromJSON(object.hash) : 0
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.child_order?.length) {
            obj.child_order = message.child_order.map((e) => Math.round(e));
        }
        if (message.child_size !== 0) {
            obj.child_size = Math.round(message.child_size);
        }
        if (message.min_prefix_length !== 0) {
            obj.min_prefix_length = Math.round(message.min_prefix_length);
        }
        if (message.max_prefix_length !== 0) {
            obj.max_prefix_length = Math.round(message.max_prefix_length);
        }
        if (message.empty_child.length !== 0) {
            obj.empty_child = base64FromBytes(message.empty_child);
        }
        if (message.hash !== 0) {
            obj.hash = hashOpToJSON(message.hash);
        }
        return obj;
    },
    create(base) {
        return exports.InnerSpec.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseInnerSpec();
        message.child_order = object.child_order?.map((e) => e) || [];
        message.child_size = object.child_size ?? 0;
        message.min_prefix_length = object.min_prefix_length ?? 0;
        message.max_prefix_length = object.max_prefix_length ?? 0;
        message.empty_child = object.empty_child ?? new Uint8Array(0);
        message.hash = object.hash ?? 0;
        return message;
    }
};
exports.BatchProof = {
    $type: "ics23.BatchProof",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.entries) {
            exports.BatchEntry.encode(v, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseBatchProof();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.entries.push(exports.BatchEntry.decode(reader, reader.uint32()));
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
            entries: globalThis.Array.isArray(object?.entries) ? object.entries.map((e) => exports.BatchEntry.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.entries?.length) {
            obj.entries = message.entries.map((e) => exports.BatchEntry.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.BatchProof.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseBatchProof();
        message.entries = object.entries?.map((e) => exports.BatchEntry.fromPartial(e)) || [];
        return message;
    }
};
exports.BatchEntry = {
    $type: "ics23.BatchEntry",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.exist !== undefined) {
            exports.ExistenceProof.encode(message.exist, writer.uint32(10).fork()).join();
        }
        if (message.nonexist !== undefined) {
            exports.NonExistenceProof.encode(message.nonexist, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseBatchEntry();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.exist = exports.ExistenceProof.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.nonexist = exports.NonExistenceProof.decode(reader, reader.uint32());
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
            exist: isSet(object.exist) ? exports.ExistenceProof.fromJSON(object.exist) : undefined,
            nonexist: isSet(object.nonexist) ? exports.NonExistenceProof.fromJSON(object.nonexist) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.exist !== undefined) {
            obj.exist = exports.ExistenceProof.toJSON(message.exist);
        }
        if (message.nonexist !== undefined) {
            obj.nonexist = exports.NonExistenceProof.toJSON(message.nonexist);
        }
        return obj;
    },
    create(base) {
        return exports.BatchEntry.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseBatchEntry();
        message.exist = object.exist !== undefined && object.exist !== null ? exports.ExistenceProof.fromPartial(object.exist) : undefined;
        message.nonexist = object.nonexist !== undefined && object.nonexist !== null ? exports.NonExistenceProof.fromPartial(object.nonexist) : undefined;
        return message;
    }
};
exports.CompressedBatchProof = {
    $type: "ics23.CompressedBatchProof",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.entries) {
            exports.CompressedBatchEntry.encode(v, writer.uint32(10).fork()).join();
        }
        for (const v of message.lookup_inners) {
            exports.InnerOp.encode(v, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseCompressedBatchProof();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.entries.push(exports.CompressedBatchEntry.decode(reader, reader.uint32()));
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.lookup_inners.push(exports.InnerOp.decode(reader, reader.uint32()));
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
            entries: globalThis.Array.isArray(object?.entries) ? object.entries.map((e) => exports.CompressedBatchEntry.fromJSON(e)) : [],
            lookup_inners: globalThis.Array.isArray(object?.lookup_inners) ? object.lookup_inners.map((e) => exports.InnerOp.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.entries?.length) {
            obj.entries = message.entries.map((e) => exports.CompressedBatchEntry.toJSON(e));
        }
        if (message.lookup_inners?.length) {
            obj.lookup_inners = message.lookup_inners.map((e) => exports.InnerOp.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.CompressedBatchProof.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseCompressedBatchProof();
        message.entries = object.entries?.map((e) => exports.CompressedBatchEntry.fromPartial(e)) || [];
        message.lookup_inners = object.lookup_inners?.map((e) => exports.InnerOp.fromPartial(e)) || [];
        return message;
    }
};
exports.CompressedBatchEntry = {
    $type: "ics23.CompressedBatchEntry",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.exist !== undefined) {
            exports.CompressedExistenceProof.encode(message.exist, writer.uint32(10).fork()).join();
        }
        if (message.nonexist !== undefined) {
            exports.CompressedNonExistenceProof.encode(message.nonexist, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseCompressedBatchEntry();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.exist = exports.CompressedExistenceProof.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.nonexist = exports.CompressedNonExistenceProof.decode(reader, reader.uint32());
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
            exist: isSet(object.exist) ? exports.CompressedExistenceProof.fromJSON(object.exist) : undefined,
            nonexist: isSet(object.nonexist) ? exports.CompressedNonExistenceProof.fromJSON(object.nonexist) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.exist !== undefined) {
            obj.exist = exports.CompressedExistenceProof.toJSON(message.exist);
        }
        if (message.nonexist !== undefined) {
            obj.nonexist = exports.CompressedNonExistenceProof.toJSON(message.nonexist);
        }
        return obj;
    },
    create(base) {
        return exports.CompressedBatchEntry.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseCompressedBatchEntry();
        message.exist = object.exist !== undefined && object.exist !== null ? exports.CompressedExistenceProof.fromPartial(object.exist) : undefined;
        message.nonexist = object.nonexist !== undefined && object.nonexist !== null ? exports.CompressedNonExistenceProof.fromPartial(object.nonexist) : undefined;
        return message;
    }
};
exports.CompressedExistenceProof = {
    $type: "ics23.CompressedExistenceProof",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.key.length !== 0) {
            writer.uint32(10).bytes(message.key);
        }
        if (message.value.length !== 0) {
            writer.uint32(18).bytes(message.value);
        }
        if (message.leaf !== undefined) {
            exports.LeafOp.encode(message.leaf, writer.uint32(26).fork()).join();
        }
        writer.uint32(34).fork();
        for (const v of message.path) {
            writer.int32(v);
        }
        writer.join();
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseCompressedExistenceProof();
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
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.leaf = exports.LeafOp.decode(reader, reader.uint32());
                    continue;
                case 4:
                    if (tag === 32) {
                        message.path.push(reader.int32());
                        continue;
                    }
                    if (tag === 34) {
                        const end2 = reader.uint32() + reader.pos;
                        while (reader.pos < end2) {
                            message.path.push(reader.int32());
                        }
                        continue;
                    }
                    break;
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
            value: isSet(object.value) ? bytesFromBase64(object.value) : new Uint8Array(0),
            leaf: isSet(object.leaf) ? exports.LeafOp.fromJSON(object.leaf) : undefined,
            path: globalThis.Array.isArray(object?.path) ? object.path.map((e) => globalThis.Number(e)) : []
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
        if (message.leaf !== undefined) {
            obj.leaf = exports.LeafOp.toJSON(message.leaf);
        }
        if (message.path?.length) {
            obj.path = message.path.map((e) => Math.round(e));
        }
        return obj;
    },
    create(base) {
        return exports.CompressedExistenceProof.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseCompressedExistenceProof();
        message.key = object.key ?? new Uint8Array(0);
        message.value = object.value ?? new Uint8Array(0);
        message.leaf = object.leaf !== undefined && object.leaf !== null ? exports.LeafOp.fromPartial(object.leaf) : undefined;
        message.path = object.path?.map((e) => e) || [];
        return message;
    }
};
exports.CompressedNonExistenceProof = {
    $type: "ics23.CompressedNonExistenceProof",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.key.length !== 0) {
            writer.uint32(10).bytes(message.key);
        }
        if (message.left !== undefined) {
            exports.CompressedExistenceProof.encode(message.left, writer.uint32(18).fork()).join();
        }
        if (message.right !== undefined) {
            exports.CompressedExistenceProof.encode(message.right, writer.uint32(26).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseCompressedNonExistenceProof();
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
                    message.left = exports.CompressedExistenceProof.decode(reader, reader.uint32());
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.right = exports.CompressedExistenceProof.decode(reader, reader.uint32());
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
            left: isSet(object.left) ? exports.CompressedExistenceProof.fromJSON(object.left) : undefined,
            right: isSet(object.right) ? exports.CompressedExistenceProof.fromJSON(object.right) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.key.length !== 0) {
            obj.key = base64FromBytes(message.key);
        }
        if (message.left !== undefined) {
            obj.left = exports.CompressedExistenceProof.toJSON(message.left);
        }
        if (message.right !== undefined) {
            obj.right = exports.CompressedExistenceProof.toJSON(message.right);
        }
        return obj;
    },
    create(base) {
        return exports.CompressedNonExistenceProof.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseCompressedNonExistenceProof();
        message.key = object.key ?? new Uint8Array(0);
        message.left = object.left !== undefined && object.left !== null ? exports.CompressedExistenceProof.fromPartial(object.left) : undefined;
        message.right = object.right !== undefined && object.right !== null ? exports.CompressedExistenceProof.fromPartial(object.right) : undefined;
        return message;
    }
};
function hashOpFromJSON(object) {
    switch (object) {
        case 0:
        case "NO_HASH":
            return confio_1.HashOp.NO_HASH;
        case 1:
        case "SHA256":
            return confio_1.HashOp.SHA256;
        case 2:
        case "SHA512":
            return confio_1.HashOp.SHA512;
        case 3:
        case "KECCAK":
            return confio_1.HashOp.KECCAK;
        case 4:
        case "RIPEMD160":
            return confio_1.HashOp.RIPEMD160;
        case 5:
        case "BITCOIN":
            return confio_1.HashOp.BITCOIN;
        case -1:
        case "UNRECOGNIZED":
        default:
            return confio_1.HashOp.UNRECOGNIZED;
    }
}
function hashOpToJSON(object) {
    switch (object) {
        case confio_1.HashOp.NO_HASH:
            return "NO_HASH";
        case confio_1.HashOp.SHA256:
            return "SHA256";
        case confio_1.HashOp.SHA512:
            return "SHA512";
        case confio_1.HashOp.KECCAK:
            return "KECCAK";
        case confio_1.HashOp.RIPEMD160:
            return "RIPEMD160";
        case confio_1.HashOp.BITCOIN:
            return "BITCOIN";
        case confio_1.HashOp.UNRECOGNIZED:
        default:
            return "UNRECOGNIZED";
    }
}
function lengthOpFromJSON(object) {
    switch (object) {
        case 0:
        case "NO_PREFIX":
            return confio_1.LengthOp.NO_PREFIX;
        case 1:
        case "VAR_PROTO":
            return confio_1.LengthOp.VAR_PROTO;
        case 2:
        case "VAR_RLP":
            return confio_1.LengthOp.VAR_RLP;
        case 3:
        case "FIXED32_BIG":
            return confio_1.LengthOp.FIXED32_BIG;
        case 4:
        case "FIXED32_LITTLE":
            return confio_1.LengthOp.FIXED32_LITTLE;
        case 5:
        case "FIXED64_BIG":
            return confio_1.LengthOp.FIXED64_BIG;
        case 6:
        case "FIXED64_LITTLE":
            return confio_1.LengthOp.FIXED64_LITTLE;
        case 7:
        case "REQUIRE_32_BYTES":
            return confio_1.LengthOp.REQUIRE_32_BYTES;
        case 8:
        case "REQUIRE_64_BYTES":
            return confio_1.LengthOp.REQUIRE_64_BYTES;
        case -1:
        case "UNRECOGNIZED":
        default:
            return confio_1.LengthOp.UNRECOGNIZED;
    }
}
function lengthOpToJSON(object) {
    switch (object) {
        case confio_1.LengthOp.NO_PREFIX:
            return "NO_PREFIX";
        case confio_1.LengthOp.VAR_PROTO:
            return "VAR_PROTO";
        case confio_1.LengthOp.VAR_RLP:
            return "VAR_RLP";
        case confio_1.LengthOp.FIXED32_BIG:
            return "FIXED32_BIG";
        case confio_1.LengthOp.FIXED32_LITTLE:
            return "FIXED32_LITTLE";
        case confio_1.LengthOp.FIXED64_BIG:
            return "FIXED64_BIG";
        case confio_1.LengthOp.FIXED64_LITTLE:
            return "FIXED64_LITTLE";
        case confio_1.LengthOp.REQUIRE_32_BYTES:
            return "REQUIRE_32_BYTES";
        case confio_1.LengthOp.REQUIRE_64_BYTES:
            return "REQUIRE_64_BYTES";
        case confio_1.LengthOp.UNRECOGNIZED:
        default:
            return "UNRECOGNIZED";
    }
}
function createBaseExistenceProof() {
    return { key: new Uint8Array(0), value: new Uint8Array(0), leaf: undefined, path: [] };
}
function createBaseNonExistenceProof() {
    return { key: new Uint8Array(0), left: undefined, right: undefined };
}
function createBaseCommitmentProof() {
    return { exist: undefined, nonexist: undefined, batch: undefined, compressed: undefined };
}
function createBaseLeafOp() {
    return { hash: 0, prehash_key: 0, prehash_value: 0, length: 0, prefix: new Uint8Array(0) };
}
function createBaseInnerOp() {
    return { hash: 0, prefix: new Uint8Array(0), suffix: new Uint8Array(0) };
}
function createBaseProofSpec() {
    return { leaf_spec: undefined, inner_spec: undefined, max_depth: 0, min_depth: 0 };
}
function createBaseInnerSpec() {
    return {
        child_order: [],
        child_size: 0,
        min_prefix_length: 0,
        max_prefix_length: 0,
        empty_child: new Uint8Array(0),
        hash: 0
    };
}
function createBaseBatchProof() {
    return { entries: [] };
}
function createBaseBatchEntry() {
    return { exist: undefined, nonexist: undefined };
}
function createBaseCompressedBatchProof() {
    return { entries: [], lookup_inners: [] };
}
function createBaseCompressedBatchEntry() {
    return { exist: undefined, nonexist: undefined };
}
function createBaseCompressedExistenceProof() {
    return { key: new Uint8Array(0), value: new Uint8Array(0), leaf: undefined, path: [] };
}
function createBaseCompressedNonExistenceProof() {
    return { key: new Uint8Array(0), left: undefined, right: undefined };
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
    ["/ics23.ExistenceProof", exports.ExistenceProof],
    ["/ics23.NonExistenceProof", exports.NonExistenceProof],
    ["/ics23.CommitmentProof", exports.CommitmentProof],
    ["/ics23.LeafOp", exports.LeafOp],
    ["/ics23.InnerOp", exports.InnerOp],
    ["/ics23.ProofSpec", exports.ProofSpec],
    ["/ics23.InnerSpec", exports.InnerSpec],
    ["/ics23.BatchProof", exports.BatchProof],
    ["/ics23.BatchEntry", exports.BatchEntry],
    ["/ics23.CompressedBatchProof", exports.CompressedBatchProof],
    ["/ics23.CompressedBatchEntry", exports.CompressedBatchEntry],
    ["/ics23.CompressedExistenceProof", exports.CompressedExistenceProof],
    ["/ics23.CompressedNonExistenceProof", exports.CompressedNonExistenceProof]
];
exports.aminoConverters = {
    "/ics23.ExistenceProof": {
        aminoType: "ics23.ExistenceProof",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/ics23.NonExistenceProof": {
        aminoType: "ics23.NonExistenceProof",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/ics23.CommitmentProof": {
        aminoType: "ics23.CommitmentProof",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/ics23.LeafOp": {
        aminoType: "ics23.LeafOp",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/ics23.InnerOp": {
        aminoType: "ics23.InnerOp",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/ics23.ProofSpec": {
        aminoType: "ics23.ProofSpec",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/ics23.InnerSpec": {
        aminoType: "ics23.InnerSpec",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/ics23.BatchProof": {
        aminoType: "ics23.BatchProof",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/ics23.BatchEntry": {
        aminoType: "ics23.BatchEntry",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/ics23.CompressedBatchProof": {
        aminoType: "ics23.CompressedBatchProof",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/ics23.CompressedBatchEntry": {
        aminoType: "ics23.CompressedBatchEntry",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/ics23.CompressedExistenceProof": {
        aminoType: "ics23.CompressedExistenceProof",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/ics23.CompressedNonExistenceProof": {
        aminoType: "ics23.CompressedNonExistenceProof",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
