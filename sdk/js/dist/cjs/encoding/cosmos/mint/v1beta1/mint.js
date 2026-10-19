"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.Params = exports.Minter = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
exports.Minter = {
    $type: "cosmos.mint.v1beta1.Minter",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.inflation !== "") {
            writer.uint32(10).string(message.inflation);
        }
        if (message.annual_provisions !== "") {
            writer.uint32(18).string(message.annual_provisions);
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
                    message.inflation = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.annual_provisions = reader.string();
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
            inflation: isSet(object.inflation) ? globalThis.String(object.inflation) : "",
            annual_provisions: isSet(object.annual_provisions) ? globalThis.String(object.annual_provisions) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.inflation !== "") {
            obj.inflation = message.inflation;
        }
        if (message.annual_provisions !== "") {
            obj.annual_provisions = message.annual_provisions;
        }
        return obj;
    },
    create(base) {
        return exports.Minter.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMinter();
        message.inflation = object.inflation ?? "";
        message.annual_provisions = object.annual_provisions ?? "";
        return message;
    }
};
exports.Params = {
    $type: "cosmos.mint.v1beta1.Params",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.mint_denom !== "") {
            writer.uint32(10).string(message.mint_denom);
        }
        if (message.inflation_rate_change !== "") {
            writer.uint32(18).string(message.inflation_rate_change);
        }
        if (message.inflation_max !== "") {
            writer.uint32(26).string(message.inflation_max);
        }
        if (message.inflation_min !== "") {
            writer.uint32(34).string(message.inflation_min);
        }
        if (message.goal_bonded !== "") {
            writer.uint32(42).string(message.goal_bonded);
        }
        if (message.blocks_per_year !== 0) {
            writer.uint32(48).uint64(message.blocks_per_year);
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
                    message.inflation_rate_change = reader.string();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.inflation_max = reader.string();
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.inflation_min = reader.string();
                    continue;
                case 5:
                    if (tag !== 42) {
                        break;
                    }
                    message.goal_bonded = reader.string();
                    continue;
                case 6:
                    if (tag !== 48) {
                        break;
                    }
                    message.blocks_per_year = longToNumber(reader.uint64());
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
            inflation_rate_change: isSet(object.inflation_rate_change) ? globalThis.String(object.inflation_rate_change) : "",
            inflation_max: isSet(object.inflation_max) ? globalThis.String(object.inflation_max) : "",
            inflation_min: isSet(object.inflation_min) ? globalThis.String(object.inflation_min) : "",
            goal_bonded: isSet(object.goal_bonded) ? globalThis.String(object.goal_bonded) : "",
            blocks_per_year: isSet(object.blocks_per_year) ? globalThis.Number(object.blocks_per_year) : 0
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.mint_denom !== "") {
            obj.mint_denom = message.mint_denom;
        }
        if (message.inflation_rate_change !== "") {
            obj.inflation_rate_change = message.inflation_rate_change;
        }
        if (message.inflation_max !== "") {
            obj.inflation_max = message.inflation_max;
        }
        if (message.inflation_min !== "") {
            obj.inflation_min = message.inflation_min;
        }
        if (message.goal_bonded !== "") {
            obj.goal_bonded = message.goal_bonded;
        }
        if (message.blocks_per_year !== 0) {
            obj.blocks_per_year = Math.round(message.blocks_per_year);
        }
        return obj;
    },
    create(base) {
        return exports.Params.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseParams();
        message.mint_denom = object.mint_denom ?? "";
        message.inflation_rate_change = object.inflation_rate_change ?? "";
        message.inflation_max = object.inflation_max ?? "";
        message.inflation_min = object.inflation_min ?? "";
        message.goal_bonded = object.goal_bonded ?? "";
        message.blocks_per_year = object.blocks_per_year ?? 0;
        return message;
    }
};
function createBaseMinter() {
    return { inflation: "", annual_provisions: "" };
}
function createBaseParams() {
    return {
        mint_denom: "",
        inflation_rate_change: "",
        inflation_max: "",
        inflation_min: "",
        goal_bonded: "",
        blocks_per_year: 0
    };
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
    ["/cosmos.mint.v1beta1.Minter", exports.Minter],
    ["/cosmos.mint.v1beta1.Params", exports.Params]
];
exports.aminoConverters = {
    "/cosmos.mint.v1beta1.Minter": {
        aminoType: "cosmos-sdk/Minter",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.mint.v1beta1.Params": {
        aminoType: "cosmos-sdk/Params",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
