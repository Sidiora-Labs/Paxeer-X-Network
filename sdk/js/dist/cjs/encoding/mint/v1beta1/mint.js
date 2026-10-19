"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.Version2Params = exports.Version2ScheduledTokenRelease = exports.Version2Minter = exports.Params = exports.ScheduledTokenRelease = exports.Minter = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
exports.Minter = {
    $type: "paxprotocol.paxchain.mint.Minter",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.start_date !== "") {
            writer.uint32(10).string(message.start_date);
        }
        if (message.end_date !== "") {
            writer.uint32(18).string(message.end_date);
        }
        if (message.denom !== "") {
            writer.uint32(26).string(message.denom);
        }
        if (message.total_mint_amount !== 0) {
            writer.uint32(32).uint64(message.total_mint_amount);
        }
        if (message.remaining_mint_amount !== 0) {
            writer.uint32(40).uint64(message.remaining_mint_amount);
        }
        if (message.last_mint_amount !== 0) {
            writer.uint32(48).uint64(message.last_mint_amount);
        }
        if (message.last_mint_date !== "") {
            writer.uint32(58).string(message.last_mint_date);
        }
        if (message.last_mint_height !== 0) {
            writer.uint32(64).uint64(message.last_mint_height);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMinter();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.start_date = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.end_date = reader.string();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.denom = reader.string();
                    continue;
                case 4:
                    if (tag !== 32) {
                        break;
                    }
                    message.total_mint_amount = longToNumber(reader.uint64());
                    continue;
                case 5:
                    if (tag !== 40) {
                        break;
                    }
                    message.remaining_mint_amount = longToNumber(reader.uint64());
                    continue;
                case 6:
                    if (tag !== 48) {
                        break;
                    }
                    message.last_mint_amount = longToNumber(reader.uint64());
                    continue;
                case 7:
                    if (tag !== 58) {
                        break;
                    }
                    message.last_mint_date = reader.string();
                    continue;
                case 8:
                    if (tag !== 64) {
                        break;
                    }
                    message.last_mint_height = longToNumber(reader.uint64());
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
            start_date: isSet(object.start_date) ? globalThis.String(object.start_date) : "",
            end_date: isSet(object.end_date) ? globalThis.String(object.end_date) : "",
            denom: isSet(object.denom) ? globalThis.String(object.denom) : "",
            total_mint_amount: isSet(object.total_mint_amount) ? globalThis.Number(object.total_mint_amount) : 0,
            remaining_mint_amount: isSet(object.remaining_mint_amount) ? globalThis.Number(object.remaining_mint_amount) : 0,
            last_mint_amount: isSet(object.last_mint_amount) ? globalThis.Number(object.last_mint_amount) : 0,
            last_mint_date: isSet(object.last_mint_date) ? globalThis.String(object.last_mint_date) : "",
            last_mint_height: isSet(object.last_mint_height) ? globalThis.Number(object.last_mint_height) : 0
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.start_date !== "") {
            obj.start_date = message.start_date;
        }
        if (message.end_date !== "") {
            obj.end_date = message.end_date;
        }
        if (message.denom !== "") {
            obj.denom = message.denom;
        }
        if (message.total_mint_amount !== 0) {
            obj.total_mint_amount = Math.round(message.total_mint_amount);
        }
        if (message.remaining_mint_amount !== 0) {
            obj.remaining_mint_amount = Math.round(message.remaining_mint_amount);
        }
        if (message.last_mint_amount !== 0) {
            obj.last_mint_amount = Math.round(message.last_mint_amount);
        }
        if (message.last_mint_date !== "") {
            obj.last_mint_date = message.last_mint_date;
        }
        if (message.last_mint_height !== 0) {
            obj.last_mint_height = Math.round(message.last_mint_height);
        }
        return obj;
    },
    create(base) {
        return exports.Minter.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMinter();
        message.start_date = object.start_date ?? "";
        message.end_date = object.end_date ?? "";
        message.denom = object.denom ?? "";
        message.total_mint_amount = object.total_mint_amount ?? 0;
        message.remaining_mint_amount = object.remaining_mint_amount ?? 0;
        message.last_mint_amount = object.last_mint_amount ?? 0;
        message.last_mint_date = object.last_mint_date ?? "";
        message.last_mint_height = object.last_mint_height ?? 0;
        return message;
    }
};
exports.ScheduledTokenRelease = {
    $type: "paxprotocol.paxchain.mint.ScheduledTokenRelease",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.start_date !== "") {
            writer.uint32(10).string(message.start_date);
        }
        if (message.end_date !== "") {
            writer.uint32(18).string(message.end_date);
        }
        if (message.token_release_amount !== 0) {
            writer.uint32(24).uint64(message.token_release_amount);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseScheduledTokenRelease();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.start_date = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.end_date = reader.string();
                    continue;
                case 3:
                    if (tag !== 24) {
                        break;
                    }
                    message.token_release_amount = longToNumber(reader.uint64());
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
            start_date: isSet(object.start_date) ? globalThis.String(object.start_date) : "",
            end_date: isSet(object.end_date) ? globalThis.String(object.end_date) : "",
            token_release_amount: isSet(object.token_release_amount) ? globalThis.Number(object.token_release_amount) : 0
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.start_date !== "") {
            obj.start_date = message.start_date;
        }
        if (message.end_date !== "") {
            obj.end_date = message.end_date;
        }
        if (message.token_release_amount !== 0) {
            obj.token_release_amount = Math.round(message.token_release_amount);
        }
        return obj;
    },
    create(base) {
        return exports.ScheduledTokenRelease.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseScheduledTokenRelease();
        message.start_date = object.start_date ?? "";
        message.end_date = object.end_date ?? "";
        message.token_release_amount = object.token_release_amount ?? 0;
        return message;
    }
};
exports.Params = {
    $type: "paxprotocol.paxchain.mint.Params",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.mint_denom !== "") {
            writer.uint32(10).string(message.mint_denom);
        }
        for (const v of message.token_release_schedule) {
            exports.ScheduledTokenRelease.encode(v, writer.uint32(18).fork()).join();
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
                    if (tag !== 10) {
                        break;
                    }
                    message.mint_denom = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.token_release_schedule.push(exports.ScheduledTokenRelease.decode(reader, reader.uint32()));
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
            mint_denom: isSet(object.mint_denom) ? globalThis.String(object.mint_denom) : "",
            token_release_schedule: globalThis.Array.isArray(object?.token_release_schedule)
                ? object.token_release_schedule.map((e) => exports.ScheduledTokenRelease.fromJSON(e))
                : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.mint_denom !== "") {
            obj.mint_denom = message.mint_denom;
        }
        if (message.token_release_schedule?.length) {
            obj.token_release_schedule = message.token_release_schedule.map((e) => exports.ScheduledTokenRelease.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.Params.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseParams();
        message.mint_denom = object.mint_denom ?? "";
        message.token_release_schedule = object.token_release_schedule?.map((e) => exports.ScheduledTokenRelease.fromPartial(e)) || [];
        return message;
    }
};
exports.Version2Minter = {
    $type: "paxprotocol.paxchain.mint.Version2Minter",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.last_mint_amount !== "") {
            writer.uint32(10).string(message.last_mint_amount);
        }
        if (message.last_mint_date !== "") {
            writer.uint32(18).string(message.last_mint_date);
        }
        if (message.last_mint_height !== 0) {
            writer.uint32(24).int64(message.last_mint_height);
        }
        if (message.denom !== "") {
            writer.uint32(34).string(message.denom);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseVersion2Minter();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.last_mint_amount = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.last_mint_date = reader.string();
                    continue;
                case 3:
                    if (tag !== 24) {
                        break;
                    }
                    message.last_mint_height = longToNumber(reader.int64());
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.denom = reader.string();
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
            last_mint_amount: isSet(object.last_mint_amount) ? globalThis.String(object.last_mint_amount) : "",
            last_mint_date: isSet(object.last_mint_date) ? globalThis.String(object.last_mint_date) : "",
            last_mint_height: isSet(object.last_mint_height) ? globalThis.Number(object.last_mint_height) : 0,
            denom: isSet(object.denom) ? globalThis.String(object.denom) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.last_mint_amount !== "") {
            obj.last_mint_amount = message.last_mint_amount;
        }
        if (message.last_mint_date !== "") {
            obj.last_mint_date = message.last_mint_date;
        }
        if (message.last_mint_height !== 0) {
            obj.last_mint_height = Math.round(message.last_mint_height);
        }
        if (message.denom !== "") {
            obj.denom = message.denom;
        }
        return obj;
    },
    create(base) {
        return exports.Version2Minter.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseVersion2Minter();
        message.last_mint_amount = object.last_mint_amount ?? "";
        message.last_mint_date = object.last_mint_date ?? "";
        message.last_mint_height = object.last_mint_height ?? 0;
        message.denom = object.denom ?? "";
        return message;
    }
};
exports.Version2ScheduledTokenRelease = {
    $type: "paxprotocol.paxchain.mint.Version2ScheduledTokenRelease",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.date !== "") {
            writer.uint32(10).string(message.date);
        }
        if (message.token_release_amount !== 0) {
            writer.uint32(16).int64(message.token_release_amount);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseVersion2ScheduledTokenRelease();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.date = reader.string();
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.token_release_amount = longToNumber(reader.int64());
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
            date: isSet(object.date) ? globalThis.String(object.date) : "",
            token_release_amount: isSet(object.token_release_amount) ? globalThis.Number(object.token_release_amount) : 0
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.date !== "") {
            obj.date = message.date;
        }
        if (message.token_release_amount !== 0) {
            obj.token_release_amount = Math.round(message.token_release_amount);
        }
        return obj;
    },
    create(base) {
        return exports.Version2ScheduledTokenRelease.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseVersion2ScheduledTokenRelease();
        message.date = object.date ?? "";
        message.token_release_amount = object.token_release_amount ?? 0;
        return message;
    }
};
exports.Version2Params = {
    $type: "paxprotocol.paxchain.mint.Version2Params",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.mint_denom !== "") {
            writer.uint32(10).string(message.mint_denom);
        }
        for (const v of message.token_release_schedule) {
            exports.Version2ScheduledTokenRelease.encode(v, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseVersion2Params();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.mint_denom = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.token_release_schedule.push(exports.Version2ScheduledTokenRelease.decode(reader, reader.uint32()));
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
            mint_denom: isSet(object.mint_denom) ? globalThis.String(object.mint_denom) : "",
            token_release_schedule: globalThis.Array.isArray(object?.token_release_schedule)
                ? object.token_release_schedule.map((e) => exports.Version2ScheduledTokenRelease.fromJSON(e))
                : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.mint_denom !== "") {
            obj.mint_denom = message.mint_denom;
        }
        if (message.token_release_schedule?.length) {
            obj.token_release_schedule = message.token_release_schedule.map((e) => exports.Version2ScheduledTokenRelease.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.Version2Params.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseVersion2Params();
        message.mint_denom = object.mint_denom ?? "";
        message.token_release_schedule = object.token_release_schedule?.map((e) => exports.Version2ScheduledTokenRelease.fromPartial(e)) || [];
        return message;
    }
};
function createBaseMinter() {
    return {
        start_date: "",
        end_date: "",
        denom: "",
        total_mint_amount: 0,
        remaining_mint_amount: 0,
        last_mint_amount: 0,
        last_mint_date: "",
        last_mint_height: 0
    };
}
function createBaseScheduledTokenRelease() {
    return { start_date: "", end_date: "", token_release_amount: 0 };
}
function createBaseParams() {
    return { mint_denom: "", token_release_schedule: [] };
}
function createBaseVersion2Minter() {
    return { last_mint_amount: "", last_mint_date: "", last_mint_height: 0, denom: "" };
}
function createBaseVersion2ScheduledTokenRelease() {
    return { date: "", token_release_amount: 0 };
}
function createBaseVersion2Params() {
    return { mint_denom: "", token_release_schedule: [] };
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
    ["/paxprotocol.paxchain.mint.Minter", exports.Minter],
    ["/paxprotocol.paxchain.mint.ScheduledTokenRelease", exports.ScheduledTokenRelease],
    ["/paxprotocol.paxchain.mint.Params", exports.Params],
    ["/paxprotocol.paxchain.mint.Version2Minter", exports.Version2Minter],
    ["/paxprotocol.paxchain.mint.Version2ScheduledTokenRelease", exports.Version2ScheduledTokenRelease],
    ["/paxprotocol.paxchain.mint.Version2Params", exports.Version2Params]
];
exports.aminoConverters = {
    "/paxprotocol.paxchain.mint.Minter": {
        aminoType: "mint/Minter",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.mint.ScheduledTokenRelease": {
        aminoType: "mint/ScheduledTokenRelease",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.mint.Params": {
        aminoType: "mint/Params",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.mint.Version2Minter": {
        aminoType: "mint/Version2Minter",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.mint.Version2ScheduledTokenRelease": {
        aminoType: "mint/Version2ScheduledTokenRelease",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.mint.Version2Params": {
        aminoType: "mint/Version2Params",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
