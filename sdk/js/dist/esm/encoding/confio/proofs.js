import { BinaryReader, BinaryWriter } from "@bufbuild/protobuf/wire";
import { HashOp, LengthOp } from "../../types/confio";
export const ExistenceProof = {
    $type: "ics23.ExistenceProof",
    encode(message, writer = new BinaryWriter()) {
        if (message.key.length !== 0) {
            writer.uint32(10).bytes(message.key);
        }
        if (message.value.length !== 0) {
            writer.uint32(18).bytes(message.value);
        }
        if (message.leaf !== undefined) {
            LeafOp.encode(message.leaf, writer.uint32(26).fork()).join();
        }
        for (const v of message.path) {
            InnerOp.encode(v, writer.uint32(34).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
                    message.leaf = LeafOp.decode(reader, reader.uint32());
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.path.push(InnerOp.decode(reader, reader.uint32()));
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
            leaf: isSet(object.leaf) ? LeafOp.fromJSON(object.leaf) : undefined,
            path: globalThis.Array.isArray(object?.path) ? object.path.map((e) => InnerOp.fromJSON(e)) : []
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
            obj.leaf = LeafOp.toJSON(message.leaf);
        }
        if (message.path?.length) {
            obj.path = message.path.map((e) => InnerOp.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return ExistenceProof.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseExistenceProof();
        message.key = object.key ?? new Uint8Array(0);
        message.value = object.value ?? new Uint8Array(0);
        message.leaf = object.leaf !== undefined && object.leaf !== null ? LeafOp.fromPartial(object.leaf) : undefined;
        message.path = object.path?.map((e) => InnerOp.fromPartial(e)) || [];
        return message;
    }
};
export const NonExistenceProof = {
    $type: "ics23.NonExistenceProof",
    encode(message, writer = new BinaryWriter()) {
        if (message.key.length !== 0) {
            writer.uint32(10).bytes(message.key);
        }
        if (message.left !== undefined) {
            ExistenceProof.encode(message.left, writer.uint32(18).fork()).join();
        }
        if (message.right !== undefined) {
            ExistenceProof.encode(message.right, writer.uint32(26).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
                    message.left = ExistenceProof.decode(reader, reader.uint32());
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.right = ExistenceProof.decode(reader, reader.uint32());
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
            left: isSet(object.left) ? ExistenceProof.fromJSON(object.left) : undefined,
            right: isSet(object.right) ? ExistenceProof.fromJSON(object.right) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.key.length !== 0) {
            obj.key = base64FromBytes(message.key);
        }
        if (message.left !== undefined) {
            obj.left = ExistenceProof.toJSON(message.left);
        }
        if (message.right !== undefined) {
            obj.right = ExistenceProof.toJSON(message.right);
        }
        return obj;
    },
    create(base) {
        return NonExistenceProof.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseNonExistenceProof();
        message.key = object.key ?? new Uint8Array(0);
        message.left = object.left !== undefined && object.left !== null ? ExistenceProof.fromPartial(object.left) : undefined;
        message.right = object.right !== undefined && object.right !== null ? ExistenceProof.fromPartial(object.right) : undefined;
        return message;
    }
};
export const CommitmentProof = {
    $type: "ics23.CommitmentProof",
    encode(message, writer = new BinaryWriter()) {
        if (message.exist !== undefined) {
            ExistenceProof.encode(message.exist, writer.uint32(10).fork()).join();
        }
        if (message.nonexist !== undefined) {
            NonExistenceProof.encode(message.nonexist, writer.uint32(18).fork()).join();
        }
        if (message.batch !== undefined) {
            BatchProof.encode(message.batch, writer.uint32(26).fork()).join();
        }
        if (message.compressed !== undefined) {
            CompressedBatchProof.encode(message.compressed, writer.uint32(34).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseCommitmentProof();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.exist = ExistenceProof.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.nonexist = NonExistenceProof.decode(reader, reader.uint32());
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.batch = BatchProof.decode(reader, reader.uint32());
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.compressed = CompressedBatchProof.decode(reader, reader.uint32());
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
            exist: isSet(object.exist) ? ExistenceProof.fromJSON(object.exist) : undefined,
            nonexist: isSet(object.nonexist) ? NonExistenceProof.fromJSON(object.nonexist) : undefined,
            batch: isSet(object.batch) ? BatchProof.fromJSON(object.batch) : undefined,
            compressed: isSet(object.compressed) ? CompressedBatchProof.fromJSON(object.compressed) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.exist !== undefined) {
            obj.exist = ExistenceProof.toJSON(message.exist);
        }
        if (message.nonexist !== undefined) {
            obj.nonexist = NonExistenceProof.toJSON(message.nonexist);
        }
        if (message.batch !== undefined) {
            obj.batch = BatchProof.toJSON(message.batch);
        }
        if (message.compressed !== undefined) {
            obj.compressed = CompressedBatchProof.toJSON(message.compressed);
        }
        return obj;
    },
    create(base) {
        return CommitmentProof.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseCommitmentProof();
        message.exist = object.exist !== undefined && object.exist !== null ? ExistenceProof.fromPartial(object.exist) : undefined;
        message.nonexist = object.nonexist !== undefined && object.nonexist !== null ? NonExistenceProof.fromPartial(object.nonexist) : undefined;
        message.batch = object.batch !== undefined && object.batch !== null ? BatchProof.fromPartial(object.batch) : undefined;
        message.compressed = object.compressed !== undefined && object.compressed !== null ? CompressedBatchProof.fromPartial(object.compressed) : undefined;
        return message;
    }
};
export const LeafOp = {
    $type: "ics23.LeafOp",
    encode(message, writer = new BinaryWriter()) {
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
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
        return LeafOp.fromPartial(base ?? {});
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
export const InnerOp = {
    $type: "ics23.InnerOp",
    encode(message, writer = new BinaryWriter()) {
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
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
        return InnerOp.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseInnerOp();
        message.hash = object.hash ?? 0;
        message.prefix = object.prefix ?? new Uint8Array(0);
        message.suffix = object.suffix ?? new Uint8Array(0);
        return message;
    }
};
export const ProofSpec = {
    $type: "ics23.ProofSpec",
    encode(message, writer = new BinaryWriter()) {
        if (message.leaf_spec !== undefined) {
            LeafOp.encode(message.leaf_spec, writer.uint32(10).fork()).join();
        }
        if (message.inner_spec !== undefined) {
            InnerSpec.encode(message.inner_spec, writer.uint32(18).fork()).join();
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
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseProofSpec();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.leaf_spec = LeafOp.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.inner_spec = InnerSpec.decode(reader, reader.uint32());
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
            leaf_spec: isSet(object.leaf_spec) ? LeafOp.fromJSON(object.leaf_spec) : undefined,
            inner_spec: isSet(object.inner_spec) ? InnerSpec.fromJSON(object.inner_spec) : undefined,
            max_depth: isSet(object.max_depth) ? globalThis.Number(object.max_depth) : 0,
            min_depth: isSet(object.min_depth) ? globalThis.Number(object.min_depth) : 0
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.leaf_spec !== undefined) {
            obj.leaf_spec = LeafOp.toJSON(message.leaf_spec);
        }
        if (message.inner_spec !== undefined) {
            obj.inner_spec = InnerSpec.toJSON(message.inner_spec);
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
        return ProofSpec.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseProofSpec();
        message.leaf_spec = object.leaf_spec !== undefined && object.leaf_spec !== null ? LeafOp.fromPartial(object.leaf_spec) : undefined;
        message.inner_spec = object.inner_spec !== undefined && object.inner_spec !== null ? InnerSpec.fromPartial(object.inner_spec) : undefined;
        message.max_depth = object.max_depth ?? 0;
        message.min_depth = object.min_depth ?? 0;
        return message;
    }
};
export const InnerSpec = {
    $type: "ics23.InnerSpec",
    encode(message, writer = new BinaryWriter()) {
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
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
        return InnerSpec.fromPartial(base ?? {});
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
export const BatchProof = {
    $type: "ics23.BatchProof",
    encode(message, writer = new BinaryWriter()) {
        for (const v of message.entries) {
            BatchEntry.encode(v, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseBatchProof();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.entries.push(BatchEntry.decode(reader, reader.uint32()));
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
            entries: globalThis.Array.isArray(object?.entries) ? object.entries.map((e) => BatchEntry.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.entries?.length) {
            obj.entries = message.entries.map((e) => BatchEntry.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return BatchProof.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseBatchProof();
        message.entries = object.entries?.map((e) => BatchEntry.fromPartial(e)) || [];
        return message;
    }
};
export const BatchEntry = {
    $type: "ics23.BatchEntry",
    encode(message, writer = new BinaryWriter()) {
        if (message.exist !== undefined) {
            ExistenceProof.encode(message.exist, writer.uint32(10).fork()).join();
        }
        if (message.nonexist !== undefined) {
            NonExistenceProof.encode(message.nonexist, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseBatchEntry();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.exist = ExistenceProof.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.nonexist = NonExistenceProof.decode(reader, reader.uint32());
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
            exist: isSet(object.exist) ? ExistenceProof.fromJSON(object.exist) : undefined,
            nonexist: isSet(object.nonexist) ? NonExistenceProof.fromJSON(object.nonexist) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.exist !== undefined) {
            obj.exist = ExistenceProof.toJSON(message.exist);
        }
        if (message.nonexist !== undefined) {
            obj.nonexist = NonExistenceProof.toJSON(message.nonexist);
        }
        return obj;
    },
    create(base) {
        return BatchEntry.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseBatchEntry();
        message.exist = object.exist !== undefined && object.exist !== null ? ExistenceProof.fromPartial(object.exist) : undefined;
        message.nonexist = object.nonexist !== undefined && object.nonexist !== null ? NonExistenceProof.fromPartial(object.nonexist) : undefined;
        return message;
    }
};
export const CompressedBatchProof = {
    $type: "ics23.CompressedBatchProof",
    encode(message, writer = new BinaryWriter()) {
        for (const v of message.entries) {
            CompressedBatchEntry.encode(v, writer.uint32(10).fork()).join();
        }
        for (const v of message.lookup_inners) {
            InnerOp.encode(v, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseCompressedBatchProof();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.entries.push(CompressedBatchEntry.decode(reader, reader.uint32()));
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.lookup_inners.push(InnerOp.decode(reader, reader.uint32()));
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
            entries: globalThis.Array.isArray(object?.entries) ? object.entries.map((e) => CompressedBatchEntry.fromJSON(e)) : [],
            lookup_inners: globalThis.Array.isArray(object?.lookup_inners) ? object.lookup_inners.map((e) => InnerOp.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.entries?.length) {
            obj.entries = message.entries.map((e) => CompressedBatchEntry.toJSON(e));
        }
        if (message.lookup_inners?.length) {
            obj.lookup_inners = message.lookup_inners.map((e) => InnerOp.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return CompressedBatchProof.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseCompressedBatchProof();
        message.entries = object.entries?.map((e) => CompressedBatchEntry.fromPartial(e)) || [];
        message.lookup_inners = object.lookup_inners?.map((e) => InnerOp.fromPartial(e)) || [];
        return message;
    }
};
export const CompressedBatchEntry = {
    $type: "ics23.CompressedBatchEntry",
    encode(message, writer = new BinaryWriter()) {
        if (message.exist !== undefined) {
            CompressedExistenceProof.encode(message.exist, writer.uint32(10).fork()).join();
        }
        if (message.nonexist !== undefined) {
            CompressedNonExistenceProof.encode(message.nonexist, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseCompressedBatchEntry();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.exist = CompressedExistenceProof.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.nonexist = CompressedNonExistenceProof.decode(reader, reader.uint32());
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
            exist: isSet(object.exist) ? CompressedExistenceProof.fromJSON(object.exist) : undefined,
            nonexist: isSet(object.nonexist) ? CompressedNonExistenceProof.fromJSON(object.nonexist) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.exist !== undefined) {
            obj.exist = CompressedExistenceProof.toJSON(message.exist);
        }
        if (message.nonexist !== undefined) {
            obj.nonexist = CompressedNonExistenceProof.toJSON(message.nonexist);
        }
        return obj;
    },
    create(base) {
        return CompressedBatchEntry.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseCompressedBatchEntry();
        message.exist = object.exist !== undefined && object.exist !== null ? CompressedExistenceProof.fromPartial(object.exist) : undefined;
        message.nonexist = object.nonexist !== undefined && object.nonexist !== null ? CompressedNonExistenceProof.fromPartial(object.nonexist) : undefined;
        return message;
    }
};
export const CompressedExistenceProof = {
    $type: "ics23.CompressedExistenceProof",
    encode(message, writer = new BinaryWriter()) {
        if (message.key.length !== 0) {
            writer.uint32(10).bytes(message.key);
        }
        if (message.value.length !== 0) {
            writer.uint32(18).bytes(message.value);
        }
        if (message.leaf !== undefined) {
            LeafOp.encode(message.leaf, writer.uint32(26).fork()).join();
        }
        writer.uint32(34).fork();
        for (const v of message.path) {
            writer.int32(v);
        }
        writer.join();
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
                    message.leaf = LeafOp.decode(reader, reader.uint32());
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
            leaf: isSet(object.leaf) ? LeafOp.fromJSON(object.leaf) : undefined,
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
            obj.leaf = LeafOp.toJSON(message.leaf);
        }
        if (message.path?.length) {
            obj.path = message.path.map((e) => Math.round(e));
        }
        return obj;
    },
    create(base) {
        return CompressedExistenceProof.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseCompressedExistenceProof();
        message.key = object.key ?? new Uint8Array(0);
        message.value = object.value ?? new Uint8Array(0);
        message.leaf = object.leaf !== undefined && object.leaf !== null ? LeafOp.fromPartial(object.leaf) : undefined;
        message.path = object.path?.map((e) => e) || [];
        return message;
    }
};
export const CompressedNonExistenceProof = {
    $type: "ics23.CompressedNonExistenceProof",
    encode(message, writer = new BinaryWriter()) {
        if (message.key.length !== 0) {
            writer.uint32(10).bytes(message.key);
        }
        if (message.left !== undefined) {
            CompressedExistenceProof.encode(message.left, writer.uint32(18).fork()).join();
        }
        if (message.right !== undefined) {
            CompressedExistenceProof.encode(message.right, writer.uint32(26).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
                    message.left = CompressedExistenceProof.decode(reader, reader.uint32());
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.right = CompressedExistenceProof.decode(reader, reader.uint32());
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
            left: isSet(object.left) ? CompressedExistenceProof.fromJSON(object.left) : undefined,
            right: isSet(object.right) ? CompressedExistenceProof.fromJSON(object.right) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.key.length !== 0) {
            obj.key = base64FromBytes(message.key);
        }
        if (message.left !== undefined) {
            obj.left = CompressedExistenceProof.toJSON(message.left);
        }
        if (message.right !== undefined) {
            obj.right = CompressedExistenceProof.toJSON(message.right);
        }
        return obj;
    },
    create(base) {
        return CompressedNonExistenceProof.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseCompressedNonExistenceProof();
        message.key = object.key ?? new Uint8Array(0);
        message.left = object.left !== undefined && object.left !== null ? CompressedExistenceProof.fromPartial(object.left) : undefined;
        message.right = object.right !== undefined && object.right !== null ? CompressedExistenceProof.fromPartial(object.right) : undefined;
        return message;
    }
};
export function hashOpFromJSON(object) {
    switch (object) {
        case 0:
        case "NO_HASH":
            return HashOp.NO_HASH;
        case 1:
        case "SHA256":
            return HashOp.SHA256;
        case 2:
        case "SHA512":
            return HashOp.SHA512;
        case 3:
        case "KECCAK":
            return HashOp.KECCAK;
        case 4:
        case "RIPEMD160":
            return HashOp.RIPEMD160;
        case 5:
        case "BITCOIN":
            return HashOp.BITCOIN;
        case -1:
        case "UNRECOGNIZED":
        default:
            return HashOp.UNRECOGNIZED;
    }
}
export function hashOpToJSON(object) {
    switch (object) {
        case HashOp.NO_HASH:
            return "NO_HASH";
        case HashOp.SHA256:
            return "SHA256";
        case HashOp.SHA512:
            return "SHA512";
        case HashOp.KECCAK:
            return "KECCAK";
        case HashOp.RIPEMD160:
            return "RIPEMD160";
        case HashOp.BITCOIN:
            return "BITCOIN";
        case HashOp.UNRECOGNIZED:
        default:
            return "UNRECOGNIZED";
    }
}
export function lengthOpFromJSON(object) {
    switch (object) {
        case 0:
        case "NO_PREFIX":
            return LengthOp.NO_PREFIX;
        case 1:
        case "VAR_PROTO":
            return LengthOp.VAR_PROTO;
        case 2:
        case "VAR_RLP":
            return LengthOp.VAR_RLP;
        case 3:
        case "FIXED32_BIG":
            return LengthOp.FIXED32_BIG;
        case 4:
        case "FIXED32_LITTLE":
            return LengthOp.FIXED32_LITTLE;
        case 5:
        case "FIXED64_BIG":
            return LengthOp.FIXED64_BIG;
        case 6:
        case "FIXED64_LITTLE":
            return LengthOp.FIXED64_LITTLE;
        case 7:
        case "REQUIRE_32_BYTES":
            return LengthOp.REQUIRE_32_BYTES;
        case 8:
        case "REQUIRE_64_BYTES":
            return LengthOp.REQUIRE_64_BYTES;
        case -1:
        case "UNRECOGNIZED":
        default:
            return LengthOp.UNRECOGNIZED;
    }
}
export function lengthOpToJSON(object) {
    switch (object) {
        case LengthOp.NO_PREFIX:
            return "NO_PREFIX";
        case LengthOp.VAR_PROTO:
            return "VAR_PROTO";
        case LengthOp.VAR_RLP:
            return "VAR_RLP";
        case LengthOp.FIXED32_BIG:
            return "FIXED32_BIG";
        case LengthOp.FIXED32_LITTLE:
            return "FIXED32_LITTLE";
        case LengthOp.FIXED64_BIG:
            return "FIXED64_BIG";
        case LengthOp.FIXED64_LITTLE:
            return "FIXED64_LITTLE";
        case LengthOp.REQUIRE_32_BYTES:
            return "REQUIRE_32_BYTES";
        case LengthOp.REQUIRE_64_BYTES:
            return "REQUIRE_64_BYTES";
        case LengthOp.UNRECOGNIZED:
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
export const registry = [
    ["/ics23.ExistenceProof", ExistenceProof],
    ["/ics23.NonExistenceProof", NonExistenceProof],
    ["/ics23.CommitmentProof", CommitmentProof],
    ["/ics23.LeafOp", LeafOp],
    ["/ics23.InnerOp", InnerOp],
    ["/ics23.ProofSpec", ProofSpec],
    ["/ics23.InnerSpec", InnerSpec],
    ["/ics23.BatchProof", BatchProof],
    ["/ics23.BatchEntry", BatchEntry],
    ["/ics23.CompressedBatchProof", CompressedBatchProof],
    ["/ics23.CompressedBatchEntry", CompressedBatchEntry],
    ["/ics23.CompressedExistenceProof", CompressedExistenceProof],
    ["/ics23.CompressedNonExistenceProof", CompressedNonExistenceProof]
];
export const aminoConverters = {
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
