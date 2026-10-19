"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.PeerAddressInfo = exports.PeerInfo = exports.NodeInfoOther = exports.NodeInfo = exports.ProtocolVersion = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
const timestamp_1 = require("../../google/protobuf/timestamp");
exports.ProtocolVersion = {
    $type: "tendermint.p2p.ProtocolVersion",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.p2p !== 0) {
            writer.uint32(8).uint64(message.p2p);
        }
        if (message.block !== 0) {
            writer.uint32(16).uint64(message.block);
        }
        if (message.app !== 0) {
            writer.uint32(24).uint64(message.app);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseProtocolVersion();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.p2p = longToNumber(reader.uint64());
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.block = longToNumber(reader.uint64());
                    continue;
                case 3:
                    if (tag !== 24) {
                        break;
                    }
                    message.app = longToNumber(reader.uint64());
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
            p2p: isSet(object.p2p) ? globalThis.Number(object.p2p) : 0,
            block: isSet(object.block) ? globalThis.Number(object.block) : 0,
            app: isSet(object.app) ? globalThis.Number(object.app) : 0
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.p2p !== 0) {
            obj.p2p = Math.round(message.p2p);
        }
        if (message.block !== 0) {
            obj.block = Math.round(message.block);
        }
        if (message.app !== 0) {
            obj.app = Math.round(message.app);
        }
        return obj;
    },
    create(base) {
        return exports.ProtocolVersion.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseProtocolVersion();
        message.p2p = object.p2p ?? 0;
        message.block = object.block ?? 0;
        message.app = object.app ?? 0;
        return message;
    }
};
exports.NodeInfo = {
    $type: "tendermint.p2p.NodeInfo",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.protocol_version !== undefined) {
            exports.ProtocolVersion.encode(message.protocol_version, writer.uint32(10).fork()).join();
        }
        if (message.node_id !== "") {
            writer.uint32(18).string(message.node_id);
        }
        if (message.listen_addr !== "") {
            writer.uint32(26).string(message.listen_addr);
        }
        if (message.network !== "") {
            writer.uint32(34).string(message.network);
        }
        if (message.version !== "") {
            writer.uint32(42).string(message.version);
        }
        if (message.channels.length !== 0) {
            writer.uint32(50).bytes(message.channels);
        }
        if (message.moniker !== "") {
            writer.uint32(58).string(message.moniker);
        }
        if (message.other !== undefined) {
            exports.NodeInfoOther.encode(message.other, writer.uint32(66).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseNodeInfo();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.protocol_version = exports.ProtocolVersion.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.node_id = reader.string();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.listen_addr = reader.string();
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.network = reader.string();
                    continue;
                case 5:
                    if (tag !== 42) {
                        break;
                    }
                    message.version = reader.string();
                    continue;
                case 6:
                    if (tag !== 50) {
                        break;
                    }
                    message.channels = reader.bytes();
                    continue;
                case 7:
                    if (tag !== 58) {
                        break;
                    }
                    message.moniker = reader.string();
                    continue;
                case 8:
                    if (tag !== 66) {
                        break;
                    }
                    message.other = exports.NodeInfoOther.decode(reader, reader.uint32());
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
            protocol_version: isSet(object.protocol_version) ? exports.ProtocolVersion.fromJSON(object.protocol_version) : undefined,
            node_id: isSet(object.node_id) ? globalThis.String(object.node_id) : "",
            listen_addr: isSet(object.listen_addr) ? globalThis.String(object.listen_addr) : "",
            network: isSet(object.network) ? globalThis.String(object.network) : "",
            version: isSet(object.version) ? globalThis.String(object.version) : "",
            channels: isSet(object.channels) ? bytesFromBase64(object.channels) : new Uint8Array(0),
            moniker: isSet(object.moniker) ? globalThis.String(object.moniker) : "",
            other: isSet(object.other) ? exports.NodeInfoOther.fromJSON(object.other) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.protocol_version !== undefined) {
            obj.protocol_version = exports.ProtocolVersion.toJSON(message.protocol_version);
        }
        if (message.node_id !== "") {
            obj.node_id = message.node_id;
        }
        if (message.listen_addr !== "") {
            obj.listen_addr = message.listen_addr;
        }
        if (message.network !== "") {
            obj.network = message.network;
        }
        if (message.version !== "") {
            obj.version = message.version;
        }
        if (message.channels.length !== 0) {
            obj.channels = base64FromBytes(message.channels);
        }
        if (message.moniker !== "") {
            obj.moniker = message.moniker;
        }
        if (message.other !== undefined) {
            obj.other = exports.NodeInfoOther.toJSON(message.other);
        }
        return obj;
    },
    create(base) {
        return exports.NodeInfo.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseNodeInfo();
        message.protocol_version =
            object.protocol_version !== undefined && object.protocol_version !== null ? exports.ProtocolVersion.fromPartial(object.protocol_version) : undefined;
        message.node_id = object.node_id ?? "";
        message.listen_addr = object.listen_addr ?? "";
        message.network = object.network ?? "";
        message.version = object.version ?? "";
        message.channels = object.channels ?? new Uint8Array(0);
        message.moniker = object.moniker ?? "";
        message.other = object.other !== undefined && object.other !== null ? exports.NodeInfoOther.fromPartial(object.other) : undefined;
        return message;
    }
};
exports.NodeInfoOther = {
    $type: "tendermint.p2p.NodeInfoOther",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.tx_index !== "") {
            writer.uint32(10).string(message.tx_index);
        }
        if (message.rpc_address !== "") {
            writer.uint32(18).string(message.rpc_address);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseNodeInfoOther();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.tx_index = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.rpc_address = reader.string();
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
            tx_index: isSet(object.tx_index) ? globalThis.String(object.tx_index) : "",
            rpc_address: isSet(object.rpc_address) ? globalThis.String(object.rpc_address) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.tx_index !== "") {
            obj.tx_index = message.tx_index;
        }
        if (message.rpc_address !== "") {
            obj.rpc_address = message.rpc_address;
        }
        return obj;
    },
    create(base) {
        return exports.NodeInfoOther.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseNodeInfoOther();
        message.tx_index = object.tx_index ?? "";
        message.rpc_address = object.rpc_address ?? "";
        return message;
    }
};
exports.PeerInfo = {
    $type: "tendermint.p2p.PeerInfo",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.id !== "") {
            writer.uint32(10).string(message.id);
        }
        for (const v of message.address_info) {
            exports.PeerAddressInfo.encode(v, writer.uint32(18).fork()).join();
        }
        if (message.last_connected !== undefined) {
            timestamp_1.Timestamp.encode(toTimestamp(message.last_connected), writer.uint32(26).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBasePeerInfo();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.id = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.address_info.push(exports.PeerAddressInfo.decode(reader, reader.uint32()));
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.last_connected = fromTimestamp(timestamp_1.Timestamp.decode(reader, reader.uint32()));
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
            id: isSet(object.id) ? globalThis.String(object.id) : "",
            address_info: globalThis.Array.isArray(object?.address_info) ? object.address_info.map((e) => exports.PeerAddressInfo.fromJSON(e)) : [],
            last_connected: isSet(object.last_connected) ? fromJsonTimestamp(object.last_connected) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.id !== "") {
            obj.id = message.id;
        }
        if (message.address_info?.length) {
            obj.address_info = message.address_info.map((e) => exports.PeerAddressInfo.toJSON(e));
        }
        if (message.last_connected !== undefined) {
            obj.last_connected = message.last_connected.toISOString();
        }
        return obj;
    },
    create(base) {
        return exports.PeerInfo.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBasePeerInfo();
        message.id = object.id ?? "";
        message.address_info = object.address_info?.map((e) => exports.PeerAddressInfo.fromPartial(e)) || [];
        message.last_connected = object.last_connected ?? undefined;
        return message;
    }
};
exports.PeerAddressInfo = {
    $type: "tendermint.p2p.PeerAddressInfo",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.address !== "") {
            writer.uint32(10).string(message.address);
        }
        if (message.last_dial_success !== undefined) {
            timestamp_1.Timestamp.encode(toTimestamp(message.last_dial_success), writer.uint32(18).fork()).join();
        }
        if (message.last_dial_failure !== undefined) {
            timestamp_1.Timestamp.encode(toTimestamp(message.last_dial_failure), writer.uint32(26).fork()).join();
        }
        if (message.dial_failures !== 0) {
            writer.uint32(32).uint32(message.dial_failures);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBasePeerAddressInfo();
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
                    message.last_dial_success = fromTimestamp(timestamp_1.Timestamp.decode(reader, reader.uint32()));
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.last_dial_failure = fromTimestamp(timestamp_1.Timestamp.decode(reader, reader.uint32()));
                    continue;
                case 4:
                    if (tag !== 32) {
                        break;
                    }
                    message.dial_failures = reader.uint32();
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
            last_dial_success: isSet(object.last_dial_success) ? fromJsonTimestamp(object.last_dial_success) : undefined,
            last_dial_failure: isSet(object.last_dial_failure) ? fromJsonTimestamp(object.last_dial_failure) : undefined,
            dial_failures: isSet(object.dial_failures) ? globalThis.Number(object.dial_failures) : 0
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.address !== "") {
            obj.address = message.address;
        }
        if (message.last_dial_success !== undefined) {
            obj.last_dial_success = message.last_dial_success.toISOString();
        }
        if (message.last_dial_failure !== undefined) {
            obj.last_dial_failure = message.last_dial_failure.toISOString();
        }
        if (message.dial_failures !== 0) {
            obj.dial_failures = Math.round(message.dial_failures);
        }
        return obj;
    },
    create(base) {
        return exports.PeerAddressInfo.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBasePeerAddressInfo();
        message.address = object.address ?? "";
        message.last_dial_success = object.last_dial_success ?? undefined;
        message.last_dial_failure = object.last_dial_failure ?? undefined;
        message.dial_failures = object.dial_failures ?? 0;
        return message;
    }
};
function createBaseProtocolVersion() {
    return { p2p: 0, block: 0, app: 0 };
}
function createBaseNodeInfo() {
    return {
        protocol_version: undefined,
        node_id: "",
        listen_addr: "",
        network: "",
        version: "",
        channels: new Uint8Array(0),
        moniker: "",
        other: undefined
    };
}
function createBaseNodeInfoOther() {
    return { tx_index: "", rpc_address: "" };
}
function createBasePeerInfo() {
    return { id: "", address_info: [], last_connected: undefined };
}
function createBasePeerAddressInfo() {
    return { address: "", last_dial_success: undefined, last_dial_failure: undefined, dial_failures: 0 };
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
function toTimestamp(date) {
    const seconds = Math.trunc(date.getTime() / 1000);
    const nanos = (date.getTime() % 1000) * 1000000;
    return { seconds, nanos };
}
function fromTimestamp(t) {
    let millis = (t.seconds || 0) * 1000;
    millis += (t.nanos || 0) / 1000000;
    return new globalThis.Date(millis);
}
function fromJsonTimestamp(o) {
    if (o instanceof globalThis.Date) {
        return o;
    }
    else if (typeof o === "string") {
        return new globalThis.Date(o);
    }
    else {
        return fromTimestamp(timestamp_1.Timestamp.fromJSON(o));
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
    ["/tendermint.p2p.ProtocolVersion", exports.ProtocolVersion],
    ["/tendermint.p2p.NodeInfo", exports.NodeInfo],
    ["/tendermint.p2p.NodeInfoOther", exports.NodeInfoOther],
    ["/tendermint.p2p.PeerInfo", exports.PeerInfo],
    ["/tendermint.p2p.PeerAddressInfo", exports.PeerAddressInfo]
];
exports.aminoConverters = {
    "/tendermint.p2p.ProtocolVersion": {
        aminoType: "tendermint.p2p.ProtocolVersion",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/tendermint.p2p.NodeInfo": {
        aminoType: "tendermint.p2p.NodeInfo",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/tendermint.p2p.NodeInfoOther": {
        aminoType: "tendermint.p2p.NodeInfoOther",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/tendermint.p2p.PeerInfo": {
        aminoType: "tendermint.p2p.PeerInfo",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/tendermint.p2p.PeerAddressInfo": {
        aminoType: "tendermint.p2p.PeerAddressInfo",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
