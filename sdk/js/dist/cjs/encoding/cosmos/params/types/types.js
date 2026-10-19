"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.GenesisState = exports.CosmosGasParams = exports.FeesParams = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
const coin_1 = require("../../base/v1beta1/coin");
exports.FeesParams = {
    $type: "cosmos.params.v1beta1.FeesParams",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.global_minimum_gas_prices) {
            coin_1.DecCoin.encode(v, writer.uint32(10).fork()).join();
        }
        for (const v of message.allowed_fee_denoms) {
            writer.uint32(18).string(v);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseFeesParams();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.global_minimum_gas_prices.push(coin_1.DecCoin.decode(reader, reader.uint32()));
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.allowed_fee_denoms.push(reader.string());
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
            global_minimum_gas_prices: globalThis.Array.isArray(object?.global_minimum_gas_prices)
                ? object.global_minimum_gas_prices.map((e) => coin_1.DecCoin.fromJSON(e))
                : [],
            allowed_fee_denoms: globalThis.Array.isArray(object?.allowed_fee_denoms) ? object.allowed_fee_denoms.map((e) => globalThis.String(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.global_minimum_gas_prices?.length) {
            obj.global_minimum_gas_prices = message.global_minimum_gas_prices.map((e) => coin_1.DecCoin.toJSON(e));
        }
        if (message.allowed_fee_denoms?.length) {
            obj.allowed_fee_denoms = message.allowed_fee_denoms;
        }
        return obj;
    },
    create(base) {
        return exports.FeesParams.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseFeesParams();
        message.global_minimum_gas_prices = object.global_minimum_gas_prices?.map((e) => coin_1.DecCoin.fromPartial(e)) || [];
        message.allowed_fee_denoms = object.allowed_fee_denoms?.map((e) => e) || [];
        return message;
    }
};
exports.CosmosGasParams = {
    $type: "cosmos.params.v1beta1.CosmosGasParams",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.cosmos_gas_multiplier_numerator !== 0) {
            writer.uint32(8).uint64(message.cosmos_gas_multiplier_numerator);
        }
        if (message.cosmos_gas_multiplier_denominator !== 0) {
            writer.uint32(16).uint64(message.cosmos_gas_multiplier_denominator);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseCosmosGasParams();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.cosmos_gas_multiplier_numerator = longToNumber(reader.uint64());
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.cosmos_gas_multiplier_denominator = longToNumber(reader.uint64());
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
            cosmos_gas_multiplier_numerator: isSet(object.cosmos_gas_multiplier_numerator) ? globalThis.Number(object.cosmos_gas_multiplier_numerator) : 0,
            cosmos_gas_multiplier_denominator: isSet(object.cosmos_gas_multiplier_denominator) ? globalThis.Number(object.cosmos_gas_multiplier_denominator) : 0
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.cosmos_gas_multiplier_numerator !== 0) {
            obj.cosmos_gas_multiplier_numerator = Math.round(message.cosmos_gas_multiplier_numerator);
        }
        if (message.cosmos_gas_multiplier_denominator !== 0) {
            obj.cosmos_gas_multiplier_denominator = Math.round(message.cosmos_gas_multiplier_denominator);
        }
        return obj;
    },
    create(base) {
        return exports.CosmosGasParams.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseCosmosGasParams();
        message.cosmos_gas_multiplier_numerator = object.cosmos_gas_multiplier_numerator ?? 0;
        message.cosmos_gas_multiplier_denominator = object.cosmos_gas_multiplier_denominator ?? 0;
        return message;
    }
};
exports.GenesisState = {
    $type: "cosmos.params.v1beta1.GenesisState",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.fees_params !== undefined) {
            exports.FeesParams.encode(message.fees_params, writer.uint32(10).fork()).join();
        }
        if (message.cosmos_gas_params !== undefined) {
            exports.CosmosGasParams.encode(message.cosmos_gas_params, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseGenesisState();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.fees_params = exports.FeesParams.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.cosmos_gas_params = exports.CosmosGasParams.decode(reader, reader.uint32());
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
            fees_params: isSet(object.fees_params) ? exports.FeesParams.fromJSON(object.fees_params) : undefined,
            cosmos_gas_params: isSet(object.cosmos_gas_params) ? exports.CosmosGasParams.fromJSON(object.cosmos_gas_params) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.fees_params !== undefined) {
            obj.fees_params = exports.FeesParams.toJSON(message.fees_params);
        }
        if (message.cosmos_gas_params !== undefined) {
            obj.cosmos_gas_params = exports.CosmosGasParams.toJSON(message.cosmos_gas_params);
        }
        return obj;
    },
    create(base) {
        return exports.GenesisState.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGenesisState();
        message.fees_params = object.fees_params !== undefined && object.fees_params !== null ? exports.FeesParams.fromPartial(object.fees_params) : undefined;
        message.cosmos_gas_params =
            object.cosmos_gas_params !== undefined && object.cosmos_gas_params !== null ? exports.CosmosGasParams.fromPartial(object.cosmos_gas_params) : undefined;
        return message;
    }
};
function createBaseFeesParams() {
    return { global_minimum_gas_prices: [], allowed_fee_denoms: [] };
}
function createBaseCosmosGasParams() {
    return { cosmos_gas_multiplier_numerator: 0, cosmos_gas_multiplier_denominator: 0 };
}
function createBaseGenesisState() {
    return { fees_params: undefined, cosmos_gas_params: undefined };
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
    ["/cosmos.params.v1beta1.FeesParams", exports.FeesParams],
    ["/cosmos.params.v1beta1.CosmosGasParams", exports.CosmosGasParams],
    ["/cosmos.params.v1beta1.GenesisState", exports.GenesisState]
];
exports.aminoConverters = {
    "/cosmos.params.v1beta1.FeesParams": {
        aminoType: "cosmos-sdk/FeesParams",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.params.v1beta1.CosmosGasParams": {
        aminoType: "cosmos-sdk/CosmosGasParams",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.params.v1beta1.GenesisState": {
        aminoType: "cosmos-sdk/GenesisState",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
