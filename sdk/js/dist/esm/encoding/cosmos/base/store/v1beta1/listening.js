import { BinaryReader, BinaryWriter } from "@bufbuild/protobuf/wire";
export const StoreKVPair = {
    $type: "cosmos.base.store.v1beta1.StoreKVPair",
    encode(message, writer = new BinaryWriter()) {
        if (message.store_key !== "") {
            writer.uint32(10).string(message.store_key);
        }
        if (message.delete !== false) {
            writer.uint32(16).bool(message.delete);
        }
        if (message.key.length !== 0) {
            writer.uint32(26).bytes(message.key);
        }
        if (message.value.length !== 0) {
            writer.uint32(34).bytes(message.value);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseStoreKVPair();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.store_key = reader.string();
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.delete = reader.bool();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.key = reader.bytes();
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.value = reader.bytes();
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
            store_key: isSet(object.store_key) ? globalThis.String(object.store_key) : "",
            delete: isSet(object.delete) ? globalThis.Boolean(object.delete) : false,
            key: isSet(object.key) ? bytesFromBase64(object.key) : new Uint8Array(0),
            value: isSet(object.value) ? bytesFromBase64(object.value) : new Uint8Array(0)
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.store_key !== "") {
            obj.store_key = message.store_key;
        }
        if (message.delete !== false) {
            obj.delete = message.delete;
        }
        if (message.key.length !== 0) {
            obj.key = base64FromBytes(message.key);
        }
        if (message.value.length !== 0) {
            obj.value = base64FromBytes(message.value);
        }
        return obj;
    },
    create(base) {
        return StoreKVPair.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseStoreKVPair();
        message.store_key = object.store_key ?? "";
        message.delete = object.delete ?? false;
        message.key = object.key ?? new Uint8Array(0);
        message.value = object.value ?? new Uint8Array(0);
        return message;
    }
};
function createBaseStoreKVPair() {
    return { store_key: "", delete: false, key: new Uint8Array(0), value: new Uint8Array(0) };
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
export const registry = [["/cosmos.base.store.v1beta1.StoreKVPair", StoreKVPair]];
export const aminoConverters = {
    "/cosmos.base.store.v1beta1.StoreKVPair": {
        aminoType: "cosmos-sdk/StoreKVPair",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
