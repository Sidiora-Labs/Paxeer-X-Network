"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.CommitID = exports.StoreInfo = exports.CommitInfo = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
exports.CommitInfo = {
    $type: "cosmos.base.store.v1beta1.CommitInfo",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.version !== 0) {
            writer.uint32(8).int64(message.version);
        }
        for (const v of message.store_infos) {
            exports.StoreInfo.encode(v, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseCommitInfo();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.version = longToNumber(reader.int64());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.store_infos.push(exports.StoreInfo.decode(reader, reader.uint32()));
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
            version: isSet(object.version) ? globalThis.Number(object.version) : 0,
            store_infos: globalThis.Array.isArray(object?.store_infos) ? object.store_infos.map((e) => exports.StoreInfo.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.version !== 0) {
            obj.version = Math.round(message.version);
        }
        if (message.store_infos?.length) {
            obj.store_infos = message.store_infos.map((e) => exports.StoreInfo.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.CommitInfo.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseCommitInfo();
        message.version = object.version ?? 0;
        message.store_infos = object.store_infos?.map((e) => exports.StoreInfo.fromPartial(e)) || [];
        return message;
    }
};
exports.StoreInfo = {
    $type: "cosmos.base.store.v1beta1.StoreInfo",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.name !== "") {
            writer.uint32(10).string(message.name);
        }
        if (message.commit_id !== undefined) {
            exports.CommitID.encode(message.commit_id, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseStoreInfo();
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
                    if (tag !== 18) {
                        break;
                    }
                    message.commit_id = exports.CommitID.decode(reader, reader.uint32());
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
            commit_id: isSet(object.commit_id) ? exports.CommitID.fromJSON(object.commit_id) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.name !== "") {
            obj.name = message.name;
        }
        if (message.commit_id !== undefined) {
            obj.commit_id = exports.CommitID.toJSON(message.commit_id);
        }
        return obj;
    },
    create(base) {
        return exports.StoreInfo.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseStoreInfo();
        message.name = object.name ?? "";
        message.commit_id = object.commit_id !== undefined && object.commit_id !== null ? exports.CommitID.fromPartial(object.commit_id) : undefined;
        return message;
    }
};
exports.CommitID = {
    $type: "cosmos.base.store.v1beta1.CommitID",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.version !== 0) {
            writer.uint32(8).int64(message.version);
        }
        if (message.hash.length !== 0) {
            writer.uint32(18).bytes(message.hash);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseCommitID();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.version = longToNumber(reader.int64());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.hash = reader.bytes();
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
            version: isSet(object.version) ? globalThis.Number(object.version) : 0,
            hash: isSet(object.hash) ? bytesFromBase64(object.hash) : new Uint8Array(0)
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.version !== 0) {
            obj.version = Math.round(message.version);
        }
        if (message.hash.length !== 0) {
            obj.hash = base64FromBytes(message.hash);
        }
        return obj;
    },
    create(base) {
        return exports.CommitID.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseCommitID();
        message.version = object.version ?? 0;
        message.hash = object.hash ?? new Uint8Array(0);
        return message;
    }
};
function createBaseCommitInfo() {
    return { version: 0, store_infos: [] };
}
function createBaseStoreInfo() {
    return { name: "", commit_id: undefined };
}
function createBaseCommitID() {
    return { version: 0, hash: new Uint8Array(0) };
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
    ["/cosmos.base.store.v1beta1.CommitInfo", exports.CommitInfo],
    ["/cosmos.base.store.v1beta1.StoreInfo", exports.StoreInfo],
    ["/cosmos.base.store.v1beta1.CommitID", exports.CommitID]
];
exports.aminoConverters = {
    "/cosmos.base.store.v1beta1.CommitInfo": {
        aminoType: "cosmos-sdk/CommitInfo",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.base.store.v1beta1.StoreInfo": {
        aminoType: "cosmos-sdk/StoreInfo",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.base.store.v1beta1.CommitID": {
        aminoType: "cosmos-sdk/CommitID",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
