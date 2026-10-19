"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.WeiBalance = exports.Balance = exports.GenesisState = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
const coin_1 = require("../../base/v1beta1/coin");
const bank_1 = require("./bank");
exports.GenesisState = {
    $type: "cosmos.bank.v1beta1.GenesisState",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.params !== undefined) {
            bank_1.Params.encode(message.params, writer.uint32(10).fork()).join();
        }
        for (const v of message.balances) {
            exports.Balance.encode(v, writer.uint32(18).fork()).join();
        }
        for (const v of message.supply) {
            coin_1.Coin.encode(v, writer.uint32(26).fork()).join();
        }
        for (const v of message.denom_metadata) {
            bank_1.Metadata.encode(v, writer.uint32(34).fork()).join();
        }
        for (const v of message.wei_balances) {
            exports.WeiBalance.encode(v, writer.uint32(42).fork()).join();
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
                    message.params = bank_1.Params.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.balances.push(exports.Balance.decode(reader, reader.uint32()));
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.supply.push(coin_1.Coin.decode(reader, reader.uint32()));
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.denom_metadata.push(bank_1.Metadata.decode(reader, reader.uint32()));
                    continue;
                case 5:
                    if (tag !== 42) {
                        break;
                    }
                    message.wei_balances.push(exports.WeiBalance.decode(reader, reader.uint32()));
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
            params: isSet(object.params) ? bank_1.Params.fromJSON(object.params) : undefined,
            balances: globalThis.Array.isArray(object?.balances) ? object.balances.map((e) => exports.Balance.fromJSON(e)) : [],
            supply: globalThis.Array.isArray(object?.supply) ? object.supply.map((e) => coin_1.Coin.fromJSON(e)) : [],
            denom_metadata: globalThis.Array.isArray(object?.denom_metadata) ? object.denom_metadata.map((e) => bank_1.Metadata.fromJSON(e)) : [],
            wei_balances: globalThis.Array.isArray(object?.wei_balances) ? object.wei_balances.map((e) => exports.WeiBalance.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.params !== undefined) {
            obj.params = bank_1.Params.toJSON(message.params);
        }
        if (message.balances?.length) {
            obj.balances = message.balances.map((e) => exports.Balance.toJSON(e));
        }
        if (message.supply?.length) {
            obj.supply = message.supply.map((e) => coin_1.Coin.toJSON(e));
        }
        if (message.denom_metadata?.length) {
            obj.denom_metadata = message.denom_metadata.map((e) => bank_1.Metadata.toJSON(e));
        }
        if (message.wei_balances?.length) {
            obj.wei_balances = message.wei_balances.map((e) => exports.WeiBalance.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.GenesisState.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGenesisState();
        message.params = object.params !== undefined && object.params !== null ? bank_1.Params.fromPartial(object.params) : undefined;
        message.balances = object.balances?.map((e) => exports.Balance.fromPartial(e)) || [];
        message.supply = object.supply?.map((e) => coin_1.Coin.fromPartial(e)) || [];
        message.denom_metadata = object.denom_metadata?.map((e) => bank_1.Metadata.fromPartial(e)) || [];
        message.wei_balances = object.wei_balances?.map((e) => exports.WeiBalance.fromPartial(e)) || [];
        return message;
    }
};
exports.Balance = {
    $type: "cosmos.bank.v1beta1.Balance",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.address !== "") {
            writer.uint32(10).string(message.address);
        }
        for (const v of message.coins) {
            coin_1.Coin.encode(v, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseBalance();
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
                    message.coins.push(coin_1.Coin.decode(reader, reader.uint32()));
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
            coins: globalThis.Array.isArray(object?.coins) ? object.coins.map((e) => coin_1.Coin.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.address !== "") {
            obj.address = message.address;
        }
        if (message.coins?.length) {
            obj.coins = message.coins.map((e) => coin_1.Coin.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.Balance.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseBalance();
        message.address = object.address ?? "";
        message.coins = object.coins?.map((e) => coin_1.Coin.fromPartial(e)) || [];
        return message;
    }
};
exports.WeiBalance = {
    $type: "cosmos.bank.v1beta1.WeiBalance",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.address !== "") {
            writer.uint32(10).string(message.address);
        }
        if (message.amount !== "") {
            writer.uint32(18).string(message.amount);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseWeiBalance();
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
                    message.amount = reader.string();
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
            amount: isSet(object.amount) ? globalThis.String(object.amount) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.address !== "") {
            obj.address = message.address;
        }
        if (message.amount !== "") {
            obj.amount = message.amount;
        }
        return obj;
    },
    create(base) {
        return exports.WeiBalance.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseWeiBalance();
        message.address = object.address ?? "";
        message.amount = object.amount ?? "";
        return message;
    }
};
function createBaseGenesisState() {
    return { params: undefined, balances: [], supply: [], denom_metadata: [], wei_balances: [] };
}
function createBaseBalance() {
    return { address: "", coins: [] };
}
function createBaseWeiBalance() {
    return { address: "", amount: "" };
}
function isSet(value) {
    return value !== null && value !== undefined;
}
exports.registry = [
    ["/cosmos.bank.v1beta1.GenesisState", exports.GenesisState],
    ["/cosmos.bank.v1beta1.Balance", exports.Balance],
    ["/cosmos.bank.v1beta1.WeiBalance", exports.WeiBalance]
];
exports.aminoConverters = {
    "/cosmos.bank.v1beta1.GenesisState": {
        aminoType: "cosmos-sdk/GenesisState",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.bank.v1beta1.Balance": {
        aminoType: "cosmos-sdk/Balance",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.bank.v1beta1.WeiBalance": {
        aminoType: "cosmos-sdk/WeiBalance",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
