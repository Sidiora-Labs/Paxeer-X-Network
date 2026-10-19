"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.SnapshotExtensionPayload = exports.SnapshotExtensionMeta = exports.SnapshotIAVLItem = exports.SnapshotStoreItem = exports.SnapshotItem = exports.Metadata = exports.Snapshot = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
exports.Snapshot = {
    $type: "cosmos.base.snapshots.v1beta1.Snapshot",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.height !== 0) {
            writer.uint32(8).uint64(message.height);
        }
        if (message.format !== 0) {
            writer.uint32(16).uint32(message.format);
        }
        if (message.chunks !== 0) {
            writer.uint32(24).uint32(message.chunks);
        }
        if (message.hash.length !== 0) {
            writer.uint32(34).bytes(message.hash);
        }
        if (message.metadata !== undefined) {
            exports.Metadata.encode(message.metadata, writer.uint32(42).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseSnapshot();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.height = longToNumber(reader.uint64());
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.format = reader.uint32();
                    continue;
                case 3:
                    if (tag !== 24) {
                        break;
                    }
                    message.chunks = reader.uint32();
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.hash = reader.bytes();
                    continue;
                case 5:
                    if (tag !== 42) {
                        break;
                    }
                    message.metadata = exports.Metadata.decode(reader, reader.uint32());
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
            height: isSet(object.height) ? globalThis.Number(object.height) : 0,
            format: isSet(object.format) ? globalThis.Number(object.format) : 0,
            chunks: isSet(object.chunks) ? globalThis.Number(object.chunks) : 0,
            hash: isSet(object.hash) ? bytesFromBase64(object.hash) : new Uint8Array(0),
            metadata: isSet(object.metadata) ? exports.Metadata.fromJSON(object.metadata) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.height !== 0) {
            obj.height = Math.round(message.height);
        }
        if (message.format !== 0) {
            obj.format = Math.round(message.format);
        }
        if (message.chunks !== 0) {
            obj.chunks = Math.round(message.chunks);
        }
        if (message.hash.length !== 0) {
            obj.hash = base64FromBytes(message.hash);
        }
        if (message.metadata !== undefined) {
            obj.metadata = exports.Metadata.toJSON(message.metadata);
        }
        return obj;
    },
    create(base) {
        return exports.Snapshot.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseSnapshot();
        message.height = object.height ?? 0;
        message.format = object.format ?? 0;
        message.chunks = object.chunks ?? 0;
        message.hash = object.hash ?? new Uint8Array(0);
        message.metadata = object.metadata !== undefined && object.metadata !== null ? exports.Metadata.fromPartial(object.metadata) : undefined;
        return message;
    }
};
exports.Metadata = {
    $type: "cosmos.base.snapshots.v1beta1.Metadata",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.chunk_hashes) {
            writer.uint32(10).bytes(v);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMetadata();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.chunk_hashes.push(reader.bytes());
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
            chunk_hashes: globalThis.Array.isArray(object?.chunk_hashes) ? object.chunk_hashes.map((e) => bytesFromBase64(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.chunk_hashes?.length) {
            obj.chunk_hashes = message.chunk_hashes.map((e) => base64FromBytes(e));
        }
        return obj;
    },
    create(base) {
        return exports.Metadata.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMetadata();
        message.chunk_hashes = object.chunk_hashes?.map((e) => e) || [];
        return message;
    }
};
exports.SnapshotItem = {
    $type: "cosmos.base.snapshots.v1beta1.SnapshotItem",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.store !== undefined) {
            exports.SnapshotStoreItem.encode(message.store, writer.uint32(10).fork()).join();
        }
        if (message.iavl !== undefined) {
            exports.SnapshotIAVLItem.encode(message.iavl, writer.uint32(18).fork()).join();
        }
        if (message.extension !== undefined) {
            exports.SnapshotExtensionMeta.encode(message.extension, writer.uint32(26).fork()).join();
        }
        if (message.extension_payload !== undefined) {
            exports.SnapshotExtensionPayload.encode(message.extension_payload, writer.uint32(34).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseSnapshotItem();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.store = exports.SnapshotStoreItem.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.iavl = exports.SnapshotIAVLItem.decode(reader, reader.uint32());
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.extension = exports.SnapshotExtensionMeta.decode(reader, reader.uint32());
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.extension_payload = exports.SnapshotExtensionPayload.decode(reader, reader.uint32());
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
            store: isSet(object.store) ? exports.SnapshotStoreItem.fromJSON(object.store) : undefined,
            iavl: isSet(object.iavl) ? exports.SnapshotIAVLItem.fromJSON(object.iavl) : undefined,
            extension: isSet(object.extension) ? exports.SnapshotExtensionMeta.fromJSON(object.extension) : undefined,
            extension_payload: isSet(object.extension_payload) ? exports.SnapshotExtensionPayload.fromJSON(object.extension_payload) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.store !== undefined) {
            obj.store = exports.SnapshotStoreItem.toJSON(message.store);
        }
        if (message.iavl !== undefined) {
            obj.iavl = exports.SnapshotIAVLItem.toJSON(message.iavl);
        }
        if (message.extension !== undefined) {
            obj.extension = exports.SnapshotExtensionMeta.toJSON(message.extension);
        }
        if (message.extension_payload !== undefined) {
            obj.extension_payload = exports.SnapshotExtensionPayload.toJSON(message.extension_payload);
        }
        return obj;
    },
    create(base) {
        return exports.SnapshotItem.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseSnapshotItem();
        message.store = object.store !== undefined && object.store !== null ? exports.SnapshotStoreItem.fromPartial(object.store) : undefined;
        message.iavl = object.iavl !== undefined && object.iavl !== null ? exports.SnapshotIAVLItem.fromPartial(object.iavl) : undefined;
        message.extension = object.extension !== undefined && object.extension !== null ? exports.SnapshotExtensionMeta.fromPartial(object.extension) : undefined;
        message.extension_payload =
            object.extension_payload !== undefined && object.extension_payload !== null ? exports.SnapshotExtensionPayload.fromPartial(object.extension_payload) : undefined;
        return message;
    }
};
exports.SnapshotStoreItem = {
    $type: "cosmos.base.snapshots.v1beta1.SnapshotStoreItem",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.name !== "") {
            writer.uint32(10).string(message.name);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseSnapshotStoreItem();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.name = reader.string();
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
        return { name: isSet(object.name) ? globalThis.String(object.name) : "" };
    },
    toJSON(message) {
        const obj = {};
        if (message.name !== "") {
            obj.name = message.name;
        }
        return obj;
    },
    create(base) {
        return exports.SnapshotStoreItem.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseSnapshotStoreItem();
        message.name = object.name ?? "";
        return message;
    }
};
exports.SnapshotIAVLItem = {
    $type: "cosmos.base.snapshots.v1beta1.SnapshotIAVLItem",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.key.length !== 0) {
            writer.uint32(10).bytes(message.key);
        }
        if (message.value.length !== 0) {
            writer.uint32(18).bytes(message.value);
        }
        if (message.version !== 0) {
            writer.uint32(24).int64(message.version);
        }
        if (message.height !== 0) {
            writer.uint32(32).int32(message.height);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseSnapshotIAVLItem();
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
                    if (tag !== 24) {
                        break;
                    }
                    message.version = longToNumber(reader.int64());
                    continue;
                case 4:
                    if (tag !== 32) {
                        break;
                    }
                    message.height = reader.int32();
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
            version: isSet(object.version) ? globalThis.Number(object.version) : 0,
            height: isSet(object.height) ? globalThis.Number(object.height) : 0
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
        if (message.version !== 0) {
            obj.version = Math.round(message.version);
        }
        if (message.height !== 0) {
            obj.height = Math.round(message.height);
        }
        return obj;
    },
    create(base) {
        return exports.SnapshotIAVLItem.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseSnapshotIAVLItem();
        message.key = object.key ?? new Uint8Array(0);
        message.value = object.value ?? new Uint8Array(0);
        message.version = object.version ?? 0;
        message.height = object.height ?? 0;
        return message;
    }
};
exports.SnapshotExtensionMeta = {
    $type: "cosmos.base.snapshots.v1beta1.SnapshotExtensionMeta",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.name !== "") {
            writer.uint32(10).string(message.name);
        }
        if (message.format !== 0) {
            writer.uint32(16).uint32(message.format);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseSnapshotExtensionMeta();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.name = reader.string();
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.format = reader.uint32();
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
            name: isSet(object.name) ? globalThis.String(object.name) : "",
            format: isSet(object.format) ? globalThis.Number(object.format) : 0
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.name !== "") {
            obj.name = message.name;
        }
        if (message.format !== 0) {
            obj.format = Math.round(message.format);
        }
        return obj;
    },
    create(base) {
        return exports.SnapshotExtensionMeta.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseSnapshotExtensionMeta();
        message.name = object.name ?? "";
        message.format = object.format ?? 0;
        return message;
    }
};
exports.SnapshotExtensionPayload = {
    $type: "cosmos.base.snapshots.v1beta1.SnapshotExtensionPayload",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.payload.length !== 0) {
            writer.uint32(10).bytes(message.payload);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseSnapshotExtensionPayload();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.payload = reader.bytes();
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
        return { payload: isSet(object.payload) ? bytesFromBase64(object.payload) : new Uint8Array(0) };
    },
    toJSON(message) {
        const obj = {};
        if (message.payload.length !== 0) {
            obj.payload = base64FromBytes(message.payload);
        }
        return obj;
    },
    create(base) {
        return exports.SnapshotExtensionPayload.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseSnapshotExtensionPayload();
        message.payload = object.payload ?? new Uint8Array(0);
        return message;
    }
};
function createBaseSnapshot() {
    return { height: 0, format: 0, chunks: 0, hash: new Uint8Array(0), metadata: undefined };
}
function createBaseMetadata() {
    return { chunk_hashes: [] };
}
function createBaseSnapshotItem() {
    return { store: undefined, iavl: undefined, extension: undefined, extension_payload: undefined };
}
function createBaseSnapshotStoreItem() {
    return { name: "" };
}
function createBaseSnapshotIAVLItem() {
    return { key: new Uint8Array(0), value: new Uint8Array(0), version: 0, height: 0 };
}
function createBaseSnapshotExtensionMeta() {
    return { name: "", format: 0 };
}
function createBaseSnapshotExtensionPayload() {
    return { payload: new Uint8Array(0) };
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
    ["/cosmos.base.snapshots.v1beta1.Snapshot", exports.Snapshot],
    ["/cosmos.base.snapshots.v1beta1.Metadata", exports.Metadata],
    ["/cosmos.base.snapshots.v1beta1.SnapshotItem", exports.SnapshotItem],
    ["/cosmos.base.snapshots.v1beta1.SnapshotStoreItem", exports.SnapshotStoreItem],
    ["/cosmos.base.snapshots.v1beta1.SnapshotIAVLItem", exports.SnapshotIAVLItem],
    ["/cosmos.base.snapshots.v1beta1.SnapshotExtensionMeta", exports.SnapshotExtensionMeta],
    ["/cosmos.base.snapshots.v1beta1.SnapshotExtensionPayload", exports.SnapshotExtensionPayload]
];
exports.aminoConverters = {
    "/cosmos.base.snapshots.v1beta1.Snapshot": {
        aminoType: "cosmos-sdk/Snapshot",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.base.snapshots.v1beta1.Metadata": {
        aminoType: "cosmos-sdk/Metadata",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.base.snapshots.v1beta1.SnapshotItem": {
        aminoType: "cosmos-sdk/SnapshotItem",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.base.snapshots.v1beta1.SnapshotStoreItem": {
        aminoType: "cosmos-sdk/SnapshotStoreItem",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.base.snapshots.v1beta1.SnapshotIAVLItem": {
        aminoType: "cosmos-sdk/SnapshotIAVLItem",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.base.snapshots.v1beta1.SnapshotExtensionMeta": {
        aminoType: "cosmos-sdk/SnapshotExtensionMeta",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.base.snapshots.v1beta1.SnapshotExtensionPayload": {
        aminoType: "cosmos-sdk/SnapshotExtensionPayload",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
