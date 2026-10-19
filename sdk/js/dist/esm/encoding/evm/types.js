import { BinaryReader, BinaryWriter } from "@bufbuild/protobuf/wire";
export const Whitelist = {
    $type: "paxprotocol.paxchain.evm.Whitelist",
    encode(message, writer = new BinaryWriter()) {
        for (const v of message.hashes) {
            writer.uint32(10).string(v);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseWhitelist();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.hashes.push(reader.string());
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
            hashes: globalThis.Array.isArray(object?.hashes) ? object.hashes.map((e) => globalThis.String(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.hashes?.length) {
            obj.hashes = message.hashes;
        }
        return obj;
    },
    create(base) {
        return Whitelist.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseWhitelist();
        message.hashes = object.hashes?.map((e) => e) || [];
        return message;
    }
};
export const DeferredInfo = {
    $type: "paxprotocol.paxchain.evm.DeferredInfo",
    encode(message, writer = new BinaryWriter()) {
        if (message.tx_index !== 0) {
            writer.uint32(8).uint32(message.tx_index);
        }
        if (message.tx_hash.length !== 0) {
            writer.uint32(18).bytes(message.tx_hash);
        }
        if (message.tx_bloom.length !== 0) {
            writer.uint32(26).bytes(message.tx_bloom);
        }
        if (message.surplus !== "") {
            writer.uint32(34).string(message.surplus);
        }
        if (message.error !== "") {
            writer.uint32(42).string(message.error);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseDeferredInfo();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.tx_index = reader.uint32();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.tx_hash = reader.bytes();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.tx_bloom = reader.bytes();
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.surplus = reader.string();
                    continue;
                case 5:
                    if (tag !== 42) {
                        break;
                    }
                    message.error = reader.string();
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
            tx_index: isSet(object.tx_index) ? globalThis.Number(object.tx_index) : 0,
            tx_hash: isSet(object.tx_hash) ? bytesFromBase64(object.tx_hash) : new Uint8Array(0),
            tx_bloom: isSet(object.tx_bloom) ? bytesFromBase64(object.tx_bloom) : new Uint8Array(0),
            surplus: isSet(object.surplus) ? globalThis.String(object.surplus) : "",
            error: isSet(object.error) ? globalThis.String(object.error) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.tx_index !== 0) {
            obj.tx_index = Math.round(message.tx_index);
        }
        if (message.tx_hash.length !== 0) {
            obj.tx_hash = base64FromBytes(message.tx_hash);
        }
        if (message.tx_bloom.length !== 0) {
            obj.tx_bloom = base64FromBytes(message.tx_bloom);
        }
        if (message.surplus !== "") {
            obj.surplus = message.surplus;
        }
        if (message.error !== "") {
            obj.error = message.error;
        }
        return obj;
    },
    create(base) {
        return DeferredInfo.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseDeferredInfo();
        message.tx_index = object.tx_index ?? 0;
        message.tx_hash = object.tx_hash ?? new Uint8Array(0);
        message.tx_bloom = object.tx_bloom ?? new Uint8Array(0);
        message.surplus = object.surplus ?? "";
        message.error = object.error ?? "";
        return message;
    }
};
function createBaseWhitelist() {
    return { hashes: [] };
}
function createBaseDeferredInfo() {
    return { tx_index: 0, tx_hash: new Uint8Array(0), tx_bloom: new Uint8Array(0), surplus: "", error: "" };
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
    ["/paxprotocol.paxchain.evm.Whitelist", Whitelist],
    ["/paxprotocol.paxchain.evm.DeferredInfo", DeferredInfo]
];
export const aminoConverters = {
    "/paxprotocol.paxchain.evm.Whitelist": {
        aminoType: "evm/Whitelist",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.evm.DeferredInfo": {
        aminoType: "evm/DeferredInfo",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
