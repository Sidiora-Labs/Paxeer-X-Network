"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.ExtensionOptionsEthereumTx = exports.BlobTxSidecar = exports.BlobTx = exports.DynamicFeeTx = exports.AccessListTx = exports.LegacyTx = exports.AssociateTx = exports.AccessTuple = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
exports.AccessTuple = {
    $type: "paxprotocol.paxchain.eth.AccessTuple",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.address !== "") {
            writer.uint32(10).string(message.address);
        }
        for (const v of message.storage_keys) {
            writer.uint32(18).string(v);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseAccessTuple();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.address = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.storage_keys.push(reader.string());
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
            address: isSet(object.address) ? globalThis.String(object.address) : "",
            storage_keys: globalThis.Array.isArray(object?.storage_keys) ? object.storage_keys.map((e) => globalThis.String(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.address !== "") {
            obj.address = message.address;
        }
        if (message.storage_keys?.length) {
            obj.storage_keys = message.storage_keys;
        }
        return obj;
    },
    create(base) {
        return exports.AccessTuple.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseAccessTuple();
        message.address = object.address ?? "";
        message.storage_keys = object.storage_keys?.map((e) => e) || [];
        return message;
    }
};
exports.AssociateTx = {
    $type: "paxprotocol.paxchain.eth.AssociateTx",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.v.length !== 0) {
            writer.uint32(10).bytes(message.v);
        }
        if (message.r.length !== 0) {
            writer.uint32(18).bytes(message.r);
        }
        if (message.s.length !== 0) {
            writer.uint32(26).bytes(message.s);
        }
        if (message.custom_message !== "") {
            writer.uint32(34).string(message.custom_message);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseAssociateTx();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.v = reader.bytes();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.r = reader.bytes();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.s = reader.bytes();
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.custom_message = reader.string();
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
            v: isSet(object.v) ? bytesFromBase64(object.v) : new Uint8Array(0),
            r: isSet(object.r) ? bytesFromBase64(object.r) : new Uint8Array(0),
            s: isSet(object.s) ? bytesFromBase64(object.s) : new Uint8Array(0),
            custom_message: isSet(object.custom_message) ? globalThis.String(object.custom_message) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.v.length !== 0) {
            obj.v = base64FromBytes(message.v);
        }
        if (message.r.length !== 0) {
            obj.r = base64FromBytes(message.r);
        }
        if (message.s.length !== 0) {
            obj.s = base64FromBytes(message.s);
        }
        if (message.custom_message !== "") {
            obj.custom_message = message.custom_message;
        }
        return obj;
    },
    create(base) {
        return exports.AssociateTx.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseAssociateTx();
        message.v = object.v ?? new Uint8Array(0);
        message.r = object.r ?? new Uint8Array(0);
        message.s = object.s ?? new Uint8Array(0);
        message.custom_message = object.custom_message ?? "";
        return message;
    }
};
exports.LegacyTx = {
    $type: "paxprotocol.paxchain.eth.LegacyTx",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.nonce !== 0) {
            writer.uint32(8).uint64(message.nonce);
        }
        if (message.gas_price !== "") {
            writer.uint32(18).string(message.gas_price);
        }
        if (message.gas_limit !== 0) {
            writer.uint32(24).uint64(message.gas_limit);
        }
        if (message.to !== "") {
            writer.uint32(34).string(message.to);
        }
        if (message.value !== "") {
            writer.uint32(42).string(message.value);
        }
        if (message.data.length !== 0) {
            writer.uint32(50).bytes(message.data);
        }
        if (message.v.length !== 0) {
            writer.uint32(58).bytes(message.v);
        }
        if (message.r.length !== 0) {
            writer.uint32(66).bytes(message.r);
        }
        if (message.s.length !== 0) {
            writer.uint32(74).bytes(message.s);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseLegacyTx();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.nonce = longToNumber(reader.uint64());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.gas_price = reader.string();
                    continue;
                case 3:
                    if (tag !== 24) {
                        break;
                    }
                    message.gas_limit = longToNumber(reader.uint64());
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.to = reader.string();
                    continue;
                case 5:
                    if (tag !== 42) {
                        break;
                    }
                    message.value = reader.string();
                    continue;
                case 6:
                    if (tag !== 50) {
                        break;
                    }
                    message.data = reader.bytes();
                    continue;
                case 7:
                    if (tag !== 58) {
                        break;
                    }
                    message.v = reader.bytes();
                    continue;
                case 8:
                    if (tag !== 66) {
                        break;
                    }
                    message.r = reader.bytes();
                    continue;
                case 9:
                    if (tag !== 74) {
                        break;
                    }
                    message.s = reader.bytes();
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
            nonce: isSet(object.nonce) ? globalThis.Number(object.nonce) : 0,
            gas_price: isSet(object.gas_price) ? globalThis.String(object.gas_price) : "",
            gas_limit: isSet(object.gas_limit) ? globalThis.Number(object.gas_limit) : 0,
            to: isSet(object.to) ? globalThis.String(object.to) : "",
            value: isSet(object.value) ? globalThis.String(object.value) : "",
            data: isSet(object.data) ? bytesFromBase64(object.data) : new Uint8Array(0),
            v: isSet(object.v) ? bytesFromBase64(object.v) : new Uint8Array(0),
            r: isSet(object.r) ? bytesFromBase64(object.r) : new Uint8Array(0),
            s: isSet(object.s) ? bytesFromBase64(object.s) : new Uint8Array(0)
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.nonce !== 0) {
            obj.nonce = Math.round(message.nonce);
        }
        if (message.gas_price !== "") {
            obj.gas_price = message.gas_price;
        }
        if (message.gas_limit !== 0) {
            obj.gas_limit = Math.round(message.gas_limit);
        }
        if (message.to !== "") {
            obj.to = message.to;
        }
        if (message.value !== "") {
            obj.value = message.value;
        }
        if (message.data.length !== 0) {
            obj.data = base64FromBytes(message.data);
        }
        if (message.v.length !== 0) {
            obj.v = base64FromBytes(message.v);
        }
        if (message.r.length !== 0) {
            obj.r = base64FromBytes(message.r);
        }
        if (message.s.length !== 0) {
            obj.s = base64FromBytes(message.s);
        }
        return obj;
    },
    create(base) {
        return exports.LegacyTx.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseLegacyTx();
        message.nonce = object.nonce ?? 0;
        message.gas_price = object.gas_price ?? "";
        message.gas_limit = object.gas_limit ?? 0;
        message.to = object.to ?? "";
        message.value = object.value ?? "";
        message.data = object.data ?? new Uint8Array(0);
        message.v = object.v ?? new Uint8Array(0);
        message.r = object.r ?? new Uint8Array(0);
        message.s = object.s ?? new Uint8Array(0);
        return message;
    }
};
exports.AccessListTx = {
    $type: "paxprotocol.paxchain.eth.AccessListTx",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.chain_id !== "") {
            writer.uint32(10).string(message.chain_id);
        }
        if (message.nonce !== 0) {
            writer.uint32(16).uint64(message.nonce);
        }
        if (message.gas_price !== "") {
            writer.uint32(26).string(message.gas_price);
        }
        if (message.gas_limit !== 0) {
            writer.uint32(32).uint64(message.gas_limit);
        }
        if (message.to !== "") {
            writer.uint32(42).string(message.to);
        }
        if (message.value !== "") {
            writer.uint32(50).string(message.value);
        }
        if (message.data.length !== 0) {
            writer.uint32(58).bytes(message.data);
        }
        for (const v of message.accesses) {
            exports.AccessTuple.encode(v, writer.uint32(66).fork()).join();
        }
        if (message.v.length !== 0) {
            writer.uint32(74).bytes(message.v);
        }
        if (message.r.length !== 0) {
            writer.uint32(82).bytes(message.r);
        }
        if (message.s.length !== 0) {
            writer.uint32(90).bytes(message.s);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseAccessListTx();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.chain_id = reader.string();
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.nonce = longToNumber(reader.uint64());
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.gas_price = reader.string();
                    continue;
                case 4:
                    if (tag !== 32) {
                        break;
                    }
                    message.gas_limit = longToNumber(reader.uint64());
                    continue;
                case 5:
                    if (tag !== 42) {
                        break;
                    }
                    message.to = reader.string();
                    continue;
                case 6:
                    if (tag !== 50) {
                        break;
                    }
                    message.value = reader.string();
                    continue;
                case 7:
                    if (tag !== 58) {
                        break;
                    }
                    message.data = reader.bytes();
                    continue;
                case 8:
                    if (tag !== 66) {
                        break;
                    }
                    message.accesses.push(exports.AccessTuple.decode(reader, reader.uint32()));
                    continue;
                case 9:
                    if (tag !== 74) {
                        break;
                    }
                    message.v = reader.bytes();
                    continue;
                case 10:
                    if (tag !== 82) {
                        break;
                    }
                    message.r = reader.bytes();
                    continue;
                case 11:
                    if (tag !== 90) {
                        break;
                    }
                    message.s = reader.bytes();
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
            chain_id: isSet(object.chain_id) ? globalThis.String(object.chain_id) : "",
            nonce: isSet(object.nonce) ? globalThis.Number(object.nonce) : 0,
            gas_price: isSet(object.gas_price) ? globalThis.String(object.gas_price) : "",
            gas_limit: isSet(object.gas_limit) ? globalThis.Number(object.gas_limit) : 0,
            to: isSet(object.to) ? globalThis.String(object.to) : "",
            value: isSet(object.value) ? globalThis.String(object.value) : "",
            data: isSet(object.data) ? bytesFromBase64(object.data) : new Uint8Array(0),
            accesses: globalThis.Array.isArray(object?.accesses) ? object.accesses.map((e) => exports.AccessTuple.fromJSON(e)) : [],
            v: isSet(object.v) ? bytesFromBase64(object.v) : new Uint8Array(0),
            r: isSet(object.r) ? bytesFromBase64(object.r) : new Uint8Array(0),
            s: isSet(object.s) ? bytesFromBase64(object.s) : new Uint8Array(0)
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.chain_id !== "") {
            obj.chain_id = message.chain_id;
        }
        if (message.nonce !== 0) {
            obj.nonce = Math.round(message.nonce);
        }
        if (message.gas_price !== "") {
            obj.gas_price = message.gas_price;
        }
        if (message.gas_limit !== 0) {
            obj.gas_limit = Math.round(message.gas_limit);
        }
        if (message.to !== "") {
            obj.to = message.to;
        }
        if (message.value !== "") {
            obj.value = message.value;
        }
        if (message.data.length !== 0) {
            obj.data = base64FromBytes(message.data);
        }
        if (message.accesses?.length) {
            obj.accesses = message.accesses.map((e) => exports.AccessTuple.toJSON(e));
        }
        if (message.v.length !== 0) {
            obj.v = base64FromBytes(message.v);
        }
        if (message.r.length !== 0) {
            obj.r = base64FromBytes(message.r);
        }
        if (message.s.length !== 0) {
            obj.s = base64FromBytes(message.s);
        }
        return obj;
    },
    create(base) {
        return exports.AccessListTx.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseAccessListTx();
        message.chain_id = object.chain_id ?? "";
        message.nonce = object.nonce ?? 0;
        message.gas_price = object.gas_price ?? "";
        message.gas_limit = object.gas_limit ?? 0;
        message.to = object.to ?? "";
        message.value = object.value ?? "";
        message.data = object.data ?? new Uint8Array(0);
        message.accesses = object.accesses?.map((e) => exports.AccessTuple.fromPartial(e)) || [];
        message.v = object.v ?? new Uint8Array(0);
        message.r = object.r ?? new Uint8Array(0);
        message.s = object.s ?? new Uint8Array(0);
        return message;
    }
};
exports.DynamicFeeTx = {
    $type: "paxprotocol.paxchain.eth.DynamicFeeTx",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.chain_id !== "") {
            writer.uint32(10).string(message.chain_id);
        }
        if (message.nonce !== 0) {
            writer.uint32(16).uint64(message.nonce);
        }
        if (message.gas_tip_cap !== "") {
            writer.uint32(26).string(message.gas_tip_cap);
        }
        if (message.gas_fee_cap !== "") {
            writer.uint32(34).string(message.gas_fee_cap);
        }
        if (message.gas_limit !== 0) {
            writer.uint32(40).uint64(message.gas_limit);
        }
        if (message.to !== "") {
            writer.uint32(50).string(message.to);
        }
        if (message.value !== "") {
            writer.uint32(58).string(message.value);
        }
        if (message.data.length !== 0) {
            writer.uint32(66).bytes(message.data);
        }
        for (const v of message.accesses) {
            exports.AccessTuple.encode(v, writer.uint32(74).fork()).join();
        }
        if (message.v.length !== 0) {
            writer.uint32(82).bytes(message.v);
        }
        if (message.r.length !== 0) {
            writer.uint32(90).bytes(message.r);
        }
        if (message.s.length !== 0) {
            writer.uint32(98).bytes(message.s);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseDynamicFeeTx();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.chain_id = reader.string();
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.nonce = longToNumber(reader.uint64());
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.gas_tip_cap = reader.string();
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.gas_fee_cap = reader.string();
                    continue;
                case 5:
                    if (tag !== 40) {
                        break;
                    }
                    message.gas_limit = longToNumber(reader.uint64());
                    continue;
                case 6:
                    if (tag !== 50) {
                        break;
                    }
                    message.to = reader.string();
                    continue;
                case 7:
                    if (tag !== 58) {
                        break;
                    }
                    message.value = reader.string();
                    continue;
                case 8:
                    if (tag !== 66) {
                        break;
                    }
                    message.data = reader.bytes();
                    continue;
                case 9:
                    if (tag !== 74) {
                        break;
                    }
                    message.accesses.push(exports.AccessTuple.decode(reader, reader.uint32()));
                    continue;
                case 10:
                    if (tag !== 82) {
                        break;
                    }
                    message.v = reader.bytes();
                    continue;
                case 11:
                    if (tag !== 90) {
                        break;
                    }
                    message.r = reader.bytes();
                    continue;
                case 12:
                    if (tag !== 98) {
                        break;
                    }
                    message.s = reader.bytes();
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
            chain_id: isSet(object.chain_id) ? globalThis.String(object.chain_id) : "",
            nonce: isSet(object.nonce) ? globalThis.Number(object.nonce) : 0,
            gas_tip_cap: isSet(object.gas_tip_cap) ? globalThis.String(object.gas_tip_cap) : "",
            gas_fee_cap: isSet(object.gas_fee_cap) ? globalThis.String(object.gas_fee_cap) : "",
            gas_limit: isSet(object.gas_limit) ? globalThis.Number(object.gas_limit) : 0,
            to: isSet(object.to) ? globalThis.String(object.to) : "",
            value: isSet(object.value) ? globalThis.String(object.value) : "",
            data: isSet(object.data) ? bytesFromBase64(object.data) : new Uint8Array(0),
            accesses: globalThis.Array.isArray(object?.accesses) ? object.accesses.map((e) => exports.AccessTuple.fromJSON(e)) : [],
            v: isSet(object.v) ? bytesFromBase64(object.v) : new Uint8Array(0),
            r: isSet(object.r) ? bytesFromBase64(object.r) : new Uint8Array(0),
            s: isSet(object.s) ? bytesFromBase64(object.s) : new Uint8Array(0)
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.chain_id !== "") {
            obj.chain_id = message.chain_id;
        }
        if (message.nonce !== 0) {
            obj.nonce = Math.round(message.nonce);
        }
        if (message.gas_tip_cap !== "") {
            obj.gas_tip_cap = message.gas_tip_cap;
        }
        if (message.gas_fee_cap !== "") {
            obj.gas_fee_cap = message.gas_fee_cap;
        }
        if (message.gas_limit !== 0) {
            obj.gas_limit = Math.round(message.gas_limit);
        }
        if (message.to !== "") {
            obj.to = message.to;
        }
        if (message.value !== "") {
            obj.value = message.value;
        }
        if (message.data.length !== 0) {
            obj.data = base64FromBytes(message.data);
        }
        if (message.accesses?.length) {
            obj.accesses = message.accesses.map((e) => exports.AccessTuple.toJSON(e));
        }
        if (message.v.length !== 0) {
            obj.v = base64FromBytes(message.v);
        }
        if (message.r.length !== 0) {
            obj.r = base64FromBytes(message.r);
        }
        if (message.s.length !== 0) {
            obj.s = base64FromBytes(message.s);
        }
        return obj;
    },
    create(base) {
        return exports.DynamicFeeTx.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseDynamicFeeTx();
        message.chain_id = object.chain_id ?? "";
        message.nonce = object.nonce ?? 0;
        message.gas_tip_cap = object.gas_tip_cap ?? "";
        message.gas_fee_cap = object.gas_fee_cap ?? "";
        message.gas_limit = object.gas_limit ?? 0;
        message.to = object.to ?? "";
        message.value = object.value ?? "";
        message.data = object.data ?? new Uint8Array(0);
        message.accesses = object.accesses?.map((e) => exports.AccessTuple.fromPartial(e)) || [];
        message.v = object.v ?? new Uint8Array(0);
        message.r = object.r ?? new Uint8Array(0);
        message.s = object.s ?? new Uint8Array(0);
        return message;
    }
};
exports.BlobTx = {
    $type: "paxprotocol.paxchain.eth.BlobTx",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.chain_id !== "") {
            writer.uint32(10).string(message.chain_id);
        }
        if (message.nonce !== 0) {
            writer.uint32(16).uint64(message.nonce);
        }
        if (message.gas_tip_cap !== "") {
            writer.uint32(26).string(message.gas_tip_cap);
        }
        if (message.gas_fee_cap !== "") {
            writer.uint32(34).string(message.gas_fee_cap);
        }
        if (message.gas_limit !== 0) {
            writer.uint32(40).uint64(message.gas_limit);
        }
        if (message.to !== "") {
            writer.uint32(50).string(message.to);
        }
        if (message.value !== "") {
            writer.uint32(58).string(message.value);
        }
        if (message.data.length !== 0) {
            writer.uint32(66).bytes(message.data);
        }
        for (const v of message.accesses) {
            exports.AccessTuple.encode(v, writer.uint32(74).fork()).join();
        }
        if (message.blob_fee_cap !== "") {
            writer.uint32(82).string(message.blob_fee_cap);
        }
        for (const v of message.blob_hashes) {
            writer.uint32(90).bytes(v);
        }
        if (message.sidecar !== undefined) {
            exports.BlobTxSidecar.encode(message.sidecar, writer.uint32(98).fork()).join();
        }
        if (message.v.length !== 0) {
            writer.uint32(106).bytes(message.v);
        }
        if (message.r.length !== 0) {
            writer.uint32(114).bytes(message.r);
        }
        if (message.s.length !== 0) {
            writer.uint32(122).bytes(message.s);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseBlobTx();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.chain_id = reader.string();
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.nonce = longToNumber(reader.uint64());
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.gas_tip_cap = reader.string();
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.gas_fee_cap = reader.string();
                    continue;
                case 5:
                    if (tag !== 40) {
                        break;
                    }
                    message.gas_limit = longToNumber(reader.uint64());
                    continue;
                case 6:
                    if (tag !== 50) {
                        break;
                    }
                    message.to = reader.string();
                    continue;
                case 7:
                    if (tag !== 58) {
                        break;
                    }
                    message.value = reader.string();
                    continue;
                case 8:
                    if (tag !== 66) {
                        break;
                    }
                    message.data = reader.bytes();
                    continue;
                case 9:
                    if (tag !== 74) {
                        break;
                    }
                    message.accesses.push(exports.AccessTuple.decode(reader, reader.uint32()));
                    continue;
                case 10:
                    if (tag !== 82) {
                        break;
                    }
                    message.blob_fee_cap = reader.string();
                    continue;
                case 11:
                    if (tag !== 90) {
                        break;
                    }
                    message.blob_hashes.push(reader.bytes());
                    continue;
                case 12:
                    if (tag !== 98) {
                        break;
                    }
                    message.sidecar = exports.BlobTxSidecar.decode(reader, reader.uint32());
                    continue;
                case 13:
                    if (tag !== 106) {
                        break;
                    }
                    message.v = reader.bytes();
                    continue;
                case 14:
                    if (tag !== 114) {
                        break;
                    }
                    message.r = reader.bytes();
                    continue;
                case 15:
                    if (tag !== 122) {
                        break;
                    }
                    message.s = reader.bytes();
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
            chain_id: isSet(object.chain_id) ? globalThis.String(object.chain_id) : "",
            nonce: isSet(object.nonce) ? globalThis.Number(object.nonce) : 0,
            gas_tip_cap: isSet(object.gas_tip_cap) ? globalThis.String(object.gas_tip_cap) : "",
            gas_fee_cap: isSet(object.gas_fee_cap) ? globalThis.String(object.gas_fee_cap) : "",
            gas_limit: isSet(object.gas_limit) ? globalThis.Number(object.gas_limit) : 0,
            to: isSet(object.to) ? globalThis.String(object.to) : "",
            value: isSet(object.value) ? globalThis.String(object.value) : "",
            data: isSet(object.data) ? bytesFromBase64(object.data) : new Uint8Array(0),
            accesses: globalThis.Array.isArray(object?.accesses) ? object.accesses.map((e) => exports.AccessTuple.fromJSON(e)) : [],
            blob_fee_cap: isSet(object.blob_fee_cap) ? globalThis.String(object.blob_fee_cap) : "",
            blob_hashes: globalThis.Array.isArray(object?.blob_hashes) ? object.blob_hashes.map((e) => bytesFromBase64(e)) : [],
            sidecar: isSet(object.sidecar) ? exports.BlobTxSidecar.fromJSON(object.sidecar) : undefined,
            v: isSet(object.v) ? bytesFromBase64(object.v) : new Uint8Array(0),
            r: isSet(object.r) ? bytesFromBase64(object.r) : new Uint8Array(0),
            s: isSet(object.s) ? bytesFromBase64(object.s) : new Uint8Array(0)
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.chain_id !== "") {
            obj.chain_id = message.chain_id;
        }
        if (message.nonce !== 0) {
            obj.nonce = Math.round(message.nonce);
        }
        if (message.gas_tip_cap !== "") {
            obj.gas_tip_cap = message.gas_tip_cap;
        }
        if (message.gas_fee_cap !== "") {
            obj.gas_fee_cap = message.gas_fee_cap;
        }
        if (message.gas_limit !== 0) {
            obj.gas_limit = Math.round(message.gas_limit);
        }
        if (message.to !== "") {
            obj.to = message.to;
        }
        if (message.value !== "") {
            obj.value = message.value;
        }
        if (message.data.length !== 0) {
            obj.data = base64FromBytes(message.data);
        }
        if (message.accesses?.length) {
            obj.accesses = message.accesses.map((e) => exports.AccessTuple.toJSON(e));
        }
        if (message.blob_fee_cap !== "") {
            obj.blob_fee_cap = message.blob_fee_cap;
        }
        if (message.blob_hashes?.length) {
            obj.blob_hashes = message.blob_hashes.map((e) => base64FromBytes(e));
        }
        if (message.sidecar !== undefined) {
            obj.sidecar = exports.BlobTxSidecar.toJSON(message.sidecar);
        }
        if (message.v.length !== 0) {
            obj.v = base64FromBytes(message.v);
        }
        if (message.r.length !== 0) {
            obj.r = base64FromBytes(message.r);
        }
        if (message.s.length !== 0) {
            obj.s = base64FromBytes(message.s);
        }
        return obj;
    },
    create(base) {
        return exports.BlobTx.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseBlobTx();
        message.chain_id = object.chain_id ?? "";
        message.nonce = object.nonce ?? 0;
        message.gas_tip_cap = object.gas_tip_cap ?? "";
        message.gas_fee_cap = object.gas_fee_cap ?? "";
        message.gas_limit = object.gas_limit ?? 0;
        message.to = object.to ?? "";
        message.value = object.value ?? "";
        message.data = object.data ?? new Uint8Array(0);
        message.accesses = object.accesses?.map((e) => exports.AccessTuple.fromPartial(e)) || [];
        message.blob_fee_cap = object.blob_fee_cap ?? "";
        message.blob_hashes = object.blob_hashes?.map((e) => e) || [];
        message.sidecar = object.sidecar !== undefined && object.sidecar !== null ? exports.BlobTxSidecar.fromPartial(object.sidecar) : undefined;
        message.v = object.v ?? new Uint8Array(0);
        message.r = object.r ?? new Uint8Array(0);
        message.s = object.s ?? new Uint8Array(0);
        return message;
    }
};
exports.BlobTxSidecar = {
    $type: "paxprotocol.paxchain.eth.BlobTxSidecar",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.blobs) {
            writer.uint32(10).bytes(v);
        }
        for (const v of message.commitments) {
            writer.uint32(18).bytes(v);
        }
        for (const v of message.proofs) {
            writer.uint32(26).bytes(v);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseBlobTxSidecar();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.blobs.push(reader.bytes());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.commitments.push(reader.bytes());
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.proofs.push(reader.bytes());
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
            blobs: globalThis.Array.isArray(object?.blobs) ? object.blobs.map((e) => bytesFromBase64(e)) : [],
            commitments: globalThis.Array.isArray(object?.commitments) ? object.commitments.map((e) => bytesFromBase64(e)) : [],
            proofs: globalThis.Array.isArray(object?.proofs) ? object.proofs.map((e) => bytesFromBase64(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.blobs?.length) {
            obj.blobs = message.blobs.map((e) => base64FromBytes(e));
        }
        if (message.commitments?.length) {
            obj.commitments = message.commitments.map((e) => base64FromBytes(e));
        }
        if (message.proofs?.length) {
            obj.proofs = message.proofs.map((e) => base64FromBytes(e));
        }
        return obj;
    },
    create(base) {
        return exports.BlobTxSidecar.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseBlobTxSidecar();
        message.blobs = object.blobs?.map((e) => e) || [];
        message.commitments = object.commitments?.map((e) => e) || [];
        message.proofs = object.proofs?.map((e) => e) || [];
        return message;
    }
};
exports.ExtensionOptionsEthereumTx = {
    $type: "paxprotocol.paxchain.eth.ExtensionOptionsEthereumTx",
    encode(_, writer = new wire_1.BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseExtensionOptionsEthereumTx();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
            }
            if ((tag & 7) === 4 || tag === 0) {
                break;
            }
            reader.skip(tag & 7);
        }
        return message;
    },
    fromJSON(_) {
        return {};
    },
    toJSON(_) {
        const obj = {};
        return obj;
    },
    create(base) {
        return exports.ExtensionOptionsEthereumTx.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseExtensionOptionsEthereumTx();
        return message;
    }
};
function createBaseAccessTuple() {
    return { address: "", storage_keys: [] };
}
function createBaseAssociateTx() {
    return { v: new Uint8Array(0), r: new Uint8Array(0), s: new Uint8Array(0), custom_message: "" };
}
function createBaseLegacyTx() {
    return {
        nonce: 0,
        gas_price: "",
        gas_limit: 0,
        to: "",
        value: "",
        data: new Uint8Array(0),
        v: new Uint8Array(0),
        r: new Uint8Array(0),
        s: new Uint8Array(0)
    };
}
function createBaseAccessListTx() {
    return {
        chain_id: "",
        nonce: 0,
        gas_price: "",
        gas_limit: 0,
        to: "",
        value: "",
        data: new Uint8Array(0),
        accesses: [],
        v: new Uint8Array(0),
        r: new Uint8Array(0),
        s: new Uint8Array(0)
    };
}
function createBaseDynamicFeeTx() {
    return {
        chain_id: "",
        nonce: 0,
        gas_tip_cap: "",
        gas_fee_cap: "",
        gas_limit: 0,
        to: "",
        value: "",
        data: new Uint8Array(0),
        accesses: [],
        v: new Uint8Array(0),
        r: new Uint8Array(0),
        s: new Uint8Array(0)
    };
}
function createBaseBlobTx() {
    return {
        chain_id: "",
        nonce: 0,
        gas_tip_cap: "",
        gas_fee_cap: "",
        gas_limit: 0,
        to: "",
        value: "",
        data: new Uint8Array(0),
        accesses: [],
        blob_fee_cap: "",
        blob_hashes: [],
        sidecar: undefined,
        v: new Uint8Array(0),
        r: new Uint8Array(0),
        s: new Uint8Array(0)
    };
}
function createBaseBlobTxSidecar() {
    return { blobs: [], commitments: [], proofs: [] };
}
function createBaseExtensionOptionsEthereumTx() {
    return {};
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
    ["/paxprotocol.paxchain.eth.AccessTuple", exports.AccessTuple],
    ["/paxprotocol.paxchain.eth.AssociateTx", exports.AssociateTx],
    ["/paxprotocol.paxchain.eth.LegacyTx", exports.LegacyTx],
    ["/paxprotocol.paxchain.eth.AccessListTx", exports.AccessListTx],
    ["/paxprotocol.paxchain.eth.DynamicFeeTx", exports.DynamicFeeTx],
    ["/paxprotocol.paxchain.eth.BlobTx", exports.BlobTx],
    ["/paxprotocol.paxchain.eth.BlobTxSidecar", exports.BlobTxSidecar],
    ["/paxprotocol.paxchain.eth.ExtensionOptionsEthereumTx", exports.ExtensionOptionsEthereumTx]
];
exports.aminoConverters = {
    "/paxprotocol.paxchain.eth.AccessTuple": {
        aminoType: "eth/AccessTuple",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.eth.AssociateTx": {
        aminoType: "eth/AssociateTx",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.eth.LegacyTx": {
        aminoType: "eth/LegacyTx",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.eth.AccessListTx": {
        aminoType: "eth/AccessListTx",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.eth.DynamicFeeTx": {
        aminoType: "eth/DynamicFeeTx",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.eth.BlobTx": {
        aminoType: "eth/BlobTx",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.eth.BlobTxSidecar": {
        aminoType: "eth/BlobTxSidecar",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.eth.ExtensionOptionsEthereumTx": {
        aminoType: "eth/ExtensionOptionsEthereumTx",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
