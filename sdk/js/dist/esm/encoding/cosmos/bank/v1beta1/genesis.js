import { BinaryReader, BinaryWriter } from "@bufbuild/protobuf/wire";
import { Coin } from "../../base/v1beta1/coin";
import { Metadata, Params } from "./bank";
export const GenesisState = {
    $type: "cosmos.bank.v1beta1.GenesisState",
    encode(message, writer = new BinaryWriter()) {
        if (message.params !== undefined) {
            Params.encode(message.params, writer.uint32(10).fork()).join();
        }
        for (const v of message.balances) {
            Balance.encode(v, writer.uint32(18).fork()).join();
        }
        for (const v of message.supply) {
            Coin.encode(v, writer.uint32(26).fork()).join();
        }
        for (const v of message.denom_metadata) {
            Metadata.encode(v, writer.uint32(34).fork()).join();
        }
        for (const v of message.wei_balances) {
            WeiBalance.encode(v, writer.uint32(42).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseGenesisState();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.params = Params.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.balances.push(Balance.decode(reader, reader.uint32()));
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.supply.push(Coin.decode(reader, reader.uint32()));
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.denom_metadata.push(Metadata.decode(reader, reader.uint32()));
                    continue;
                case 5:
                    if (tag !== 42) {
                        break;
                    }
                    message.wei_balances.push(WeiBalance.decode(reader, reader.uint32()));
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
            params: isSet(object.params) ? Params.fromJSON(object.params) : undefined,
            balances: globalThis.Array.isArray(object?.balances) ? object.balances.map((e) => Balance.fromJSON(e)) : [],
            supply: globalThis.Array.isArray(object?.supply) ? object.supply.map((e) => Coin.fromJSON(e)) : [],
            denom_metadata: globalThis.Array.isArray(object?.denom_metadata) ? object.denom_metadata.map((e) => Metadata.fromJSON(e)) : [],
            wei_balances: globalThis.Array.isArray(object?.wei_balances) ? object.wei_balances.map((e) => WeiBalance.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.params !== undefined) {
            obj.params = Params.toJSON(message.params);
        }
        if (message.balances?.length) {
            obj.balances = message.balances.map((e) => Balance.toJSON(e));
        }
        if (message.supply?.length) {
            obj.supply = message.supply.map((e) => Coin.toJSON(e));
        }
        if (message.denom_metadata?.length) {
            obj.denom_metadata = message.denom_metadata.map((e) => Metadata.toJSON(e));
        }
        if (message.wei_balances?.length) {
            obj.wei_balances = message.wei_balances.map((e) => WeiBalance.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return GenesisState.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGenesisState();
        message.params = object.params !== undefined && object.params !== null ? Params.fromPartial(object.params) : undefined;
        message.balances = object.balances?.map((e) => Balance.fromPartial(e)) || [];
        message.supply = object.supply?.map((e) => Coin.fromPartial(e)) || [];
        message.denom_metadata = object.denom_metadata?.map((e) => Metadata.fromPartial(e)) || [];
        message.wei_balances = object.wei_balances?.map((e) => WeiBalance.fromPartial(e)) || [];
        return message;
    }
};
export const Balance = {
    $type: "cosmos.bank.v1beta1.Balance",
    encode(message, writer = new BinaryWriter()) {
        if (message.address !== "") {
            writer.uint32(10).string(message.address);
        }
        for (const v of message.coins) {
            Coin.encode(v, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
                    message.coins.push(Coin.decode(reader, reader.uint32()));
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
            coins: globalThis.Array.isArray(object?.coins) ? object.coins.map((e) => Coin.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.address !== "") {
            obj.address = message.address;
        }
        if (message.coins?.length) {
            obj.coins = message.coins.map((e) => Coin.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return Balance.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseBalance();
        message.address = object.address ?? "";
        message.coins = object.coins?.map((e) => Coin.fromPartial(e)) || [];
        return message;
    }
};
export const WeiBalance = {
    $type: "cosmos.bank.v1beta1.WeiBalance",
    encode(message, writer = new BinaryWriter()) {
        if (message.address !== "") {
            writer.uint32(10).string(message.address);
        }
        if (message.amount !== "") {
            writer.uint32(18).string(message.amount);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
        return WeiBalance.fromPartial(base ?? {});
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
export const registry = [
    ["/cosmos.bank.v1beta1.GenesisState", GenesisState],
    ["/cosmos.bank.v1beta1.Balance", Balance],
    ["/cosmos.bank.v1beta1.WeiBalance", WeiBalance]
];
export const aminoConverters = {
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
