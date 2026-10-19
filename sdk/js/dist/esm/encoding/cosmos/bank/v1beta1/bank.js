import { BinaryReader, BinaryWriter } from "@bufbuild/protobuf/wire";
import { Coin } from "../../base/v1beta1/coin";
export const Params = {
    $type: "cosmos.bank.v1beta1.Params",
    encode(message, writer = new BinaryWriter()) {
        for (const v of message.send_enabled) {
            SendEnabled.encode(v, writer.uint32(10).fork()).join();
        }
        if (message.default_send_enabled !== false) {
            writer.uint32(16).bool(message.default_send_enabled);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseParams();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.send_enabled.push(SendEnabled.decode(reader, reader.uint32()));
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.default_send_enabled = reader.bool();
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
            send_enabled: globalThis.Array.isArray(object?.send_enabled) ? object.send_enabled.map((e) => SendEnabled.fromJSON(e)) : [],
            default_send_enabled: isSet(object.default_send_enabled) ? globalThis.Boolean(object.default_send_enabled) : false
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.send_enabled?.length) {
            obj.send_enabled = message.send_enabled.map((e) => SendEnabled.toJSON(e));
        }
        if (message.default_send_enabled !== false) {
            obj.default_send_enabled = message.default_send_enabled;
        }
        return obj;
    },
    create(base) {
        return Params.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseParams();
        message.send_enabled = object.send_enabled?.map((e) => SendEnabled.fromPartial(e)) || [];
        message.default_send_enabled = object.default_send_enabled ?? false;
        return message;
    }
};
export const SendEnabled = {
    $type: "cosmos.bank.v1beta1.SendEnabled",
    encode(message, writer = new BinaryWriter()) {
        if (message.denom !== "") {
            writer.uint32(10).string(message.denom);
        }
        if (message.enabled !== false) {
            writer.uint32(16).bool(message.enabled);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseSendEnabled();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.denom = reader.string();
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.enabled = reader.bool();
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
            denom: isSet(object.denom) ? globalThis.String(object.denom) : "",
            enabled: isSet(object.enabled) ? globalThis.Boolean(object.enabled) : false
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.denom !== "") {
            obj.denom = message.denom;
        }
        if (message.enabled !== false) {
            obj.enabled = message.enabled;
        }
        return obj;
    },
    create(base) {
        return SendEnabled.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseSendEnabled();
        message.denom = object.denom ?? "";
        message.enabled = object.enabled ?? false;
        return message;
    }
};
export const Input = {
    $type: "cosmos.bank.v1beta1.Input",
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
        const message = createBaseInput();
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
        return Input.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseInput();
        message.address = object.address ?? "";
        message.coins = object.coins?.map((e) => Coin.fromPartial(e)) || [];
        return message;
    }
};
export const Output = {
    $type: "cosmos.bank.v1beta1.Output",
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
        const message = createBaseOutput();
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
        return Output.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseOutput();
        message.address = object.address ?? "";
        message.coins = object.coins?.map((e) => Coin.fromPartial(e)) || [];
        return message;
    }
};
export const Supply = {
    $type: "cosmos.bank.v1beta1.Supply",
    encode(message, writer = new BinaryWriter()) {
        for (const v of message.total) {
            Coin.encode(v, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseSupply();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.total.push(Coin.decode(reader, reader.uint32()));
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
        return { total: globalThis.Array.isArray(object?.total) ? object.total.map((e) => Coin.fromJSON(e)) : [] };
    },
    toJSON(message) {
        const obj = {};
        if (message.total?.length) {
            obj.total = message.total.map((e) => Coin.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return Supply.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseSupply();
        message.total = object.total?.map((e) => Coin.fromPartial(e)) || [];
        return message;
    }
};
export const DenomUnit = {
    $type: "cosmos.bank.v1beta1.DenomUnit",
    encode(message, writer = new BinaryWriter()) {
        if (message.denom !== "") {
            writer.uint32(10).string(message.denom);
        }
        if (message.exponent !== 0) {
            writer.uint32(16).uint32(message.exponent);
        }
        for (const v of message.aliases) {
            writer.uint32(26).string(v);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseDenomUnit();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.denom = reader.string();
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.exponent = reader.uint32();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.aliases.push(reader.string());
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
            denom: isSet(object.denom) ? globalThis.String(object.denom) : "",
            exponent: isSet(object.exponent) ? globalThis.Number(object.exponent) : 0,
            aliases: globalThis.Array.isArray(object?.aliases) ? object.aliases.map((e) => globalThis.String(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.denom !== "") {
            obj.denom = message.denom;
        }
        if (message.exponent !== 0) {
            obj.exponent = Math.round(message.exponent);
        }
        if (message.aliases?.length) {
            obj.aliases = message.aliases;
        }
        return obj;
    },
    create(base) {
        return DenomUnit.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseDenomUnit();
        message.denom = object.denom ?? "";
        message.exponent = object.exponent ?? 0;
        message.aliases = object.aliases?.map((e) => e) || [];
        return message;
    }
};
export const Metadata = {
    $type: "cosmos.bank.v1beta1.Metadata",
    encode(message, writer = new BinaryWriter()) {
        if (message.description !== "") {
            writer.uint32(10).string(message.description);
        }
        for (const v of message.denom_units) {
            DenomUnit.encode(v, writer.uint32(18).fork()).join();
        }
        if (message.base !== "") {
            writer.uint32(26).string(message.base);
        }
        if (message.display !== "") {
            writer.uint32(34).string(message.display);
        }
        if (message.name !== "") {
            writer.uint32(42).string(message.name);
        }
        if (message.symbol !== "") {
            writer.uint32(50).string(message.symbol);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMetadata();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.description = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.denom_units.push(DenomUnit.decode(reader, reader.uint32()));
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.base = reader.string();
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.display = reader.string();
                    continue;
                case 5:
                    if (tag !== 42) {
                        break;
                    }
                    message.name = reader.string();
                    continue;
                case 6:
                    if (tag !== 50) {
                        break;
                    }
                    message.symbol = reader.string();
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
            description: isSet(object.description) ? globalThis.String(object.description) : "",
            denom_units: globalThis.Array.isArray(object?.denom_units) ? object.denom_units.map((e) => DenomUnit.fromJSON(e)) : [],
            base: isSet(object.base) ? globalThis.String(object.base) : "",
            display: isSet(object.display) ? globalThis.String(object.display) : "",
            name: isSet(object.name) ? globalThis.String(object.name) : "",
            symbol: isSet(object.symbol) ? globalThis.String(object.symbol) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.description !== "") {
            obj.description = message.description;
        }
        if (message.denom_units?.length) {
            obj.denom_units = message.denom_units.map((e) => DenomUnit.toJSON(e));
        }
        if (message.base !== "") {
            obj.base = message.base;
        }
        if (message.display !== "") {
            obj.display = message.display;
        }
        if (message.name !== "") {
            obj.name = message.name;
        }
        if (message.symbol !== "") {
            obj.symbol = message.symbol;
        }
        return obj;
    },
    create(base) {
        return Metadata.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMetadata();
        message.description = object.description ?? "";
        message.denom_units = object.denom_units?.map((e) => DenomUnit.fromPartial(e)) || [];
        message.base = object.base ?? "";
        message.display = object.display ?? "";
        message.name = object.name ?? "";
        message.symbol = object.symbol ?? "";
        return message;
    }
};
export const AllowList = {
    $type: "cosmos.bank.v1beta1.AllowList",
    encode(message, writer = new BinaryWriter()) {
        for (const v of message.addresses) {
            writer.uint32(10).string(v);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseAllowList();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.addresses.push(reader.string());
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
            addresses: globalThis.Array.isArray(object?.addresses) ? object.addresses.map((e) => globalThis.String(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.addresses?.length) {
            obj.addresses = message.addresses;
        }
        return obj;
    },
    create(base) {
        return AllowList.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseAllowList();
        message.addresses = object.addresses?.map((e) => e) || [];
        return message;
    }
};
function createBaseParams() {
    return { send_enabled: [], default_send_enabled: false };
}
function createBaseSendEnabled() {
    return { denom: "", enabled: false };
}
function createBaseInput() {
    return { address: "", coins: [] };
}
function createBaseOutput() {
    return { address: "", coins: [] };
}
function createBaseSupply() {
    return { total: [] };
}
function createBaseDenomUnit() {
    return { denom: "", exponent: 0, aliases: [] };
}
function createBaseMetadata() {
    return { description: "", denom_units: [], base: "", display: "", name: "", symbol: "" };
}
function createBaseAllowList() {
    return { addresses: [] };
}
function isSet(value) {
    return value !== null && value !== undefined;
}
export const registry = [
    ["/cosmos.bank.v1beta1.Params", Params],
    ["/cosmos.bank.v1beta1.SendEnabled", SendEnabled],
    ["/cosmos.bank.v1beta1.Input", Input],
    ["/cosmos.bank.v1beta1.Output", Output],
    ["/cosmos.bank.v1beta1.Supply", Supply],
    ["/cosmos.bank.v1beta1.DenomUnit", DenomUnit],
    ["/cosmos.bank.v1beta1.Metadata", Metadata],
    ["/cosmos.bank.v1beta1.AllowList", AllowList]
];
export const aminoConverters = {
    "/cosmos.bank.v1beta1.Params": {
        aminoType: "cosmos-sdk/Params",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.bank.v1beta1.SendEnabled": {
        aminoType: "cosmos-sdk/SendEnabled",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.bank.v1beta1.Input": {
        aminoType: "cosmos-sdk/Input",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.bank.v1beta1.Output": {
        aminoType: "cosmos-sdk/Output",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.bank.v1beta1.Supply": {
        aminoType: "cosmos-sdk/Supply",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.bank.v1beta1.DenomUnit": {
        aminoType: "cosmos-sdk/DenomUnit",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.bank.v1beta1.Metadata": {
        aminoType: "cosmos-sdk/Metadata",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.bank.v1beta1.AllowList": {
        aminoType: "cosmos-sdk/AllowList",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
