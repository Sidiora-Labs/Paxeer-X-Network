"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.Params = exports.ValidatorMissedBlockArray = exports.ValidatorMissedBlockArrayLegacyMissedHeights = exports.ValidatorSigningInfo = exports.ValidatorSigningInfoLegacyMissedHeights = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
const duration_1 = require("../../../google/protobuf/duration");
const timestamp_1 = require("../../../google/protobuf/timestamp");
exports.ValidatorSigningInfoLegacyMissedHeights = {
    $type: "cosmos.slashing.v1beta1.ValidatorSigningInfoLegacyMissedHeights",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.address !== "") {
            writer.uint32(10).string(message.address);
        }
        if (message.start_height !== 0) {
            writer.uint32(16).int64(message.start_height);
        }
        if (message.jailed_until !== undefined) {
            timestamp_1.Timestamp.encode(toTimestamp(message.jailed_until), writer.uint32(26).fork()).join();
        }
        if (message.tombstoned !== false) {
            writer.uint32(32).bool(message.tombstoned);
        }
        if (message.missed_blocks_counter !== 0) {
            writer.uint32(40).int64(message.missed_blocks_counter);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseValidatorSigningInfoLegacyMissedHeights();
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
                    if (tag !== 16) {
                        break;
                    }
                    message.start_height = longToNumber(reader.int64());
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.jailed_until = fromTimestamp(timestamp_1.Timestamp.decode(reader, reader.uint32()));
                    continue;
                case 4:
                    if (tag !== 32) {
                        break;
                    }
                    message.tombstoned = reader.bool();
                    continue;
                case 5:
                    if (tag !== 40) {
                        break;
                    }
                    message.missed_blocks_counter = longToNumber(reader.int64());
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
            start_height: isSet(object.start_height) ? globalThis.Number(object.start_height) : 0,
            jailed_until: isSet(object.jailed_until) ? fromJsonTimestamp(object.jailed_until) : undefined,
            tombstoned: isSet(object.tombstoned) ? globalThis.Boolean(object.tombstoned) : false,
            missed_blocks_counter: isSet(object.missed_blocks_counter) ? globalThis.Number(object.missed_blocks_counter) : 0
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.address !== "") {
            obj.address = message.address;
        }
        if (message.start_height !== 0) {
            obj.start_height = Math.round(message.start_height);
        }
        if (message.jailed_until !== undefined) {
            obj.jailed_until = message.jailed_until.toISOString();
        }
        if (message.tombstoned !== false) {
            obj.tombstoned = message.tombstoned;
        }
        if (message.missed_blocks_counter !== 0) {
            obj.missed_blocks_counter = Math.round(message.missed_blocks_counter);
        }
        return obj;
    },
    create(base) {
        return exports.ValidatorSigningInfoLegacyMissedHeights.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseValidatorSigningInfoLegacyMissedHeights();
        message.address = object.address ?? "";
        message.start_height = object.start_height ?? 0;
        message.jailed_until = object.jailed_until ?? undefined;
        message.tombstoned = object.tombstoned ?? false;
        message.missed_blocks_counter = object.missed_blocks_counter ?? 0;
        return message;
    }
};
exports.ValidatorSigningInfo = {
    $type: "cosmos.slashing.v1beta1.ValidatorSigningInfo",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.address !== "") {
            writer.uint32(10).string(message.address);
        }
        if (message.start_height !== 0) {
            writer.uint32(16).int64(message.start_height);
        }
        if (message.index_offset !== 0) {
            writer.uint32(24).int64(message.index_offset);
        }
        if (message.jailed_until !== undefined) {
            timestamp_1.Timestamp.encode(toTimestamp(message.jailed_until), writer.uint32(34).fork()).join();
        }
        if (message.tombstoned !== false) {
            writer.uint32(40).bool(message.tombstoned);
        }
        if (message.missed_blocks_counter !== 0) {
            writer.uint32(48).int64(message.missed_blocks_counter);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseValidatorSigningInfo();
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
                    if (tag !== 16) {
                        break;
                    }
                    message.start_height = longToNumber(reader.int64());
                    continue;
                case 3:
                    if (tag !== 24) {
                        break;
                    }
                    message.index_offset = longToNumber(reader.int64());
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.jailed_until = fromTimestamp(timestamp_1.Timestamp.decode(reader, reader.uint32()));
                    continue;
                case 5:
                    if (tag !== 40) {
                        break;
                    }
                    message.tombstoned = reader.bool();
                    continue;
                case 6:
                    if (tag !== 48) {
                        break;
                    }
                    message.missed_blocks_counter = longToNumber(reader.int64());
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
            start_height: isSet(object.start_height) ? globalThis.Number(object.start_height) : 0,
            index_offset: isSet(object.index_offset) ? globalThis.Number(object.index_offset) : 0,
            jailed_until: isSet(object.jailed_until) ? fromJsonTimestamp(object.jailed_until) : undefined,
            tombstoned: isSet(object.tombstoned) ? globalThis.Boolean(object.tombstoned) : false,
            missed_blocks_counter: isSet(object.missed_blocks_counter) ? globalThis.Number(object.missed_blocks_counter) : 0
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.address !== "") {
            obj.address = message.address;
        }
        if (message.start_height !== 0) {
            obj.start_height = Math.round(message.start_height);
        }
        if (message.index_offset !== 0) {
            obj.index_offset = Math.round(message.index_offset);
        }
        if (message.jailed_until !== undefined) {
            obj.jailed_until = message.jailed_until.toISOString();
        }
        if (message.tombstoned !== false) {
            obj.tombstoned = message.tombstoned;
        }
        if (message.missed_blocks_counter !== 0) {
            obj.missed_blocks_counter = Math.round(message.missed_blocks_counter);
        }
        return obj;
    },
    create(base) {
        return exports.ValidatorSigningInfo.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseValidatorSigningInfo();
        message.address = object.address ?? "";
        message.start_height = object.start_height ?? 0;
        message.index_offset = object.index_offset ?? 0;
        message.jailed_until = object.jailed_until ?? undefined;
        message.tombstoned = object.tombstoned ?? false;
        message.missed_blocks_counter = object.missed_blocks_counter ?? 0;
        return message;
    }
};
exports.ValidatorMissedBlockArrayLegacyMissedHeights = {
    $type: "cosmos.slashing.v1beta1.ValidatorMissedBlockArrayLegacyMissedHeights",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.address !== "") {
            writer.uint32(10).string(message.address);
        }
        writer.uint32(18).fork();
        for (const v of message.missed_heights) {
            writer.int64(v);
        }
        writer.join();
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseValidatorMissedBlockArrayLegacyMissedHeights();
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
                    if (tag === 16) {
                        message.missed_heights.push(longToNumber(reader.int64()));
                        continue;
                    }
                    if (tag === 18) {
                        const end2 = reader.uint32() + reader.pos;
                        while (reader.pos < end2) {
                            message.missed_heights.push(longToNumber(reader.int64()));
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
            address: isSet(object.address) ? globalThis.String(object.address) : "",
            missed_heights: globalThis.Array.isArray(object?.missed_heights) ? object.missed_heights.map((e) => globalThis.Number(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.address !== "") {
            obj.address = message.address;
        }
        if (message.missed_heights?.length) {
            obj.missed_heights = message.missed_heights.map((e) => Math.round(e));
        }
        return obj;
    },
    create(base) {
        return exports.ValidatorMissedBlockArrayLegacyMissedHeights.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseValidatorMissedBlockArrayLegacyMissedHeights();
        message.address = object.address ?? "";
        message.missed_heights = object.missed_heights?.map((e) => e) || [];
        return message;
    }
};
exports.ValidatorMissedBlockArray = {
    $type: "cosmos.slashing.v1beta1.ValidatorMissedBlockArray",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.address !== "") {
            writer.uint32(10).string(message.address);
        }
        if (message.window_size !== 0) {
            writer.uint32(16).int64(message.window_size);
        }
        writer.uint32(26).fork();
        for (const v of message.missed_blocks) {
            writer.uint64(v);
        }
        writer.join();
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseValidatorMissedBlockArray();
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
                    if (tag !== 16) {
                        break;
                    }
                    message.window_size = longToNumber(reader.int64());
                    continue;
                case 3:
                    if (tag === 24) {
                        message.missed_blocks.push(longToNumber(reader.uint64()));
                        continue;
                    }
                    if (tag === 26) {
                        const end2 = reader.uint32() + reader.pos;
                        while (reader.pos < end2) {
                            message.missed_blocks.push(longToNumber(reader.uint64()));
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
            address: isSet(object.address) ? globalThis.String(object.address) : "",
            window_size: isSet(object.window_size) ? globalThis.Number(object.window_size) : 0,
            missed_blocks: globalThis.Array.isArray(object?.missed_blocks) ? object.missed_blocks.map((e) => globalThis.Number(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.address !== "") {
            obj.address = message.address;
        }
        if (message.window_size !== 0) {
            obj.window_size = Math.round(message.window_size);
        }
        if (message.missed_blocks?.length) {
            obj.missed_blocks = message.missed_blocks.map((e) => Math.round(e));
        }
        return obj;
    },
    create(base) {
        return exports.ValidatorMissedBlockArray.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseValidatorMissedBlockArray();
        message.address = object.address ?? "";
        message.window_size = object.window_size ?? 0;
        message.missed_blocks = object.missed_blocks?.map((e) => e) || [];
        return message;
    }
};
exports.Params = {
    $type: "cosmos.slashing.v1beta1.Params",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.signed_blocks_window !== 0) {
            writer.uint32(8).int64(message.signed_blocks_window);
        }
        if (message.min_signed_per_window.length !== 0) {
            writer.uint32(18).bytes(message.min_signed_per_window);
        }
        if (message.downtime_jail_duration !== undefined) {
            duration_1.Duration.encode(message.downtime_jail_duration, writer.uint32(26).fork()).join();
        }
        if (message.slash_fraction_double_sign.length !== 0) {
            writer.uint32(34).bytes(message.slash_fraction_double_sign);
        }
        if (message.slash_fraction_downtime.length !== 0) {
            writer.uint32(42).bytes(message.slash_fraction_downtime);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseParams();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.signed_blocks_window = longToNumber(reader.int64());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.min_signed_per_window = reader.bytes();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.downtime_jail_duration = duration_1.Duration.decode(reader, reader.uint32());
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.slash_fraction_double_sign = reader.bytes();
                    continue;
                case 5:
                    if (tag !== 42) {
                        break;
                    }
                    message.slash_fraction_downtime = reader.bytes();
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
            signed_blocks_window: isSet(object.signed_blocks_window) ? globalThis.Number(object.signed_blocks_window) : 0,
            min_signed_per_window: isSet(object.min_signed_per_window) ? bytesFromBase64(object.min_signed_per_window) : new Uint8Array(0),
            downtime_jail_duration: isSet(object.downtime_jail_duration) ? duration_1.Duration.fromJSON(object.downtime_jail_duration) : undefined,
            slash_fraction_double_sign: isSet(object.slash_fraction_double_sign) ? bytesFromBase64(object.slash_fraction_double_sign) : new Uint8Array(0),
            slash_fraction_downtime: isSet(object.slash_fraction_downtime) ? bytesFromBase64(object.slash_fraction_downtime) : new Uint8Array(0)
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.signed_blocks_window !== 0) {
            obj.signed_blocks_window = Math.round(message.signed_blocks_window);
        }
        if (message.min_signed_per_window.length !== 0) {
            obj.min_signed_per_window = base64FromBytes(message.min_signed_per_window);
        }
        if (message.downtime_jail_duration !== undefined) {
            obj.downtime_jail_duration = duration_1.Duration.toJSON(message.downtime_jail_duration);
        }
        if (message.slash_fraction_double_sign.length !== 0) {
            obj.slash_fraction_double_sign = base64FromBytes(message.slash_fraction_double_sign);
        }
        if (message.slash_fraction_downtime.length !== 0) {
            obj.slash_fraction_downtime = base64FromBytes(message.slash_fraction_downtime);
        }
        return obj;
    },
    create(base) {
        return exports.Params.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseParams();
        message.signed_blocks_window = object.signed_blocks_window ?? 0;
        message.min_signed_per_window = object.min_signed_per_window ?? new Uint8Array(0);
        message.downtime_jail_duration =
            object.downtime_jail_duration !== undefined && object.downtime_jail_duration !== null ? duration_1.Duration.fromPartial(object.downtime_jail_duration) : undefined;
        message.slash_fraction_double_sign = object.slash_fraction_double_sign ?? new Uint8Array(0);
        message.slash_fraction_downtime = object.slash_fraction_downtime ?? new Uint8Array(0);
        return message;
    }
};
function createBaseValidatorSigningInfoLegacyMissedHeights() {
    return { address: "", start_height: 0, jailed_until: undefined, tombstoned: false, missed_blocks_counter: 0 };
}
function createBaseValidatorSigningInfo() {
    return {
        address: "",
        start_height: 0,
        index_offset: 0,
        jailed_until: undefined,
        tombstoned: false,
        missed_blocks_counter: 0
    };
}
function createBaseValidatorMissedBlockArrayLegacyMissedHeights() {
    return { address: "", missed_heights: [] };
}
function createBaseValidatorMissedBlockArray() {
    return { address: "", window_size: 0, missed_blocks: [] };
}
function createBaseParams() {
    return {
        signed_blocks_window: 0,
        min_signed_per_window: new Uint8Array(0),
        downtime_jail_duration: undefined,
        slash_fraction_double_sign: new Uint8Array(0),
        slash_fraction_downtime: new Uint8Array(0)
    };
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
    ["/cosmos.slashing.v1beta1.ValidatorSigningInfoLegacyMissedHeights", exports.ValidatorSigningInfoLegacyMissedHeights],
    ["/cosmos.slashing.v1beta1.ValidatorSigningInfo", exports.ValidatorSigningInfo],
    ["/cosmos.slashing.v1beta1.ValidatorMissedBlockArrayLegacyMissedHeights", exports.ValidatorMissedBlockArrayLegacyMissedHeights],
    ["/cosmos.slashing.v1beta1.ValidatorMissedBlockArray", exports.ValidatorMissedBlockArray],
    ["/cosmos.slashing.v1beta1.Params", exports.Params]
];
exports.aminoConverters = {
    "/cosmos.slashing.v1beta1.ValidatorSigningInfoLegacyMissedHeights": {
        aminoType: "cosmos-sdk/ValidatorSigningInfoLegacyMissedHeights",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.slashing.v1beta1.ValidatorSigningInfo": {
        aminoType: "cosmos-sdk/ValidatorSigningInfo",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.slashing.v1beta1.ValidatorMissedBlockArrayLegacyMissedHeights": {
        aminoType: "cosmos-sdk/ValidatorMissedBlockArrayLegacyMissedHeights",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.slashing.v1beta1.ValidatorMissedBlockArray": {
        aminoType: "cosmos-sdk/ValidatorMissedBlockArray",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.slashing.v1beta1.Params": {
        aminoType: "cosmos-sdk/Params",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
