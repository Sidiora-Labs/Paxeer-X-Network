import { BinaryReader, BinaryWriter } from "@bufbuild/protobuf/wire";
import { Params } from "./params";
export const AddressAssociation = {
    $type: "paxprotocol.paxchain.evm.AddressAssociation",
    encode(message, writer = new BinaryWriter()) {
        if (message.pax_address !== "") {
            writer.uint32(10).string(message.pax_address);
        }
        if (message.eth_address !== "") {
            writer.uint32(18).string(message.eth_address);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseAddressAssociation();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.pax_address = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.eth_address = reader.string();
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
            pax_address: isSet(object.pax_address) ? globalThis.String(object.pax_address) : "",
            eth_address: isSet(object.eth_address) ? globalThis.String(object.eth_address) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.pax_address !== "") {
            obj.pax_address = message.pax_address;
        }
        if (message.eth_address !== "") {
            obj.eth_address = message.eth_address;
        }
        return obj;
    },
    create(base) {
        return AddressAssociation.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseAddressAssociation();
        message.pax_address = object.pax_address ?? "";
        message.eth_address = object.eth_address ?? "";
        return message;
    }
};
export const Code = {
    $type: "paxprotocol.paxchain.evm.Code",
    encode(message, writer = new BinaryWriter()) {
        if (message.address !== "") {
            writer.uint32(10).string(message.address);
        }
        if (message.code.length !== 0) {
            writer.uint32(18).bytes(message.code);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseCode();
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
                    message.code = reader.bytes();
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
            code: isSet(object.code) ? bytesFromBase64(object.code) : new Uint8Array(0)
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.address !== "") {
            obj.address = message.address;
        }
        if (message.code.length !== 0) {
            obj.code = base64FromBytes(message.code);
        }
        return obj;
    },
    create(base) {
        return Code.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseCode();
        message.address = object.address ?? "";
        message.code = object.code ?? new Uint8Array(0);
        return message;
    }
};
export const ContractState = {
    $type: "paxprotocol.paxchain.evm.ContractState",
    encode(message, writer = new BinaryWriter()) {
        if (message.address !== "") {
            writer.uint32(10).string(message.address);
        }
        if (message.key.length !== 0) {
            writer.uint32(18).bytes(message.key);
        }
        if (message.value.length !== 0) {
            writer.uint32(26).bytes(message.value);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseContractState();
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
                    message.key = reader.bytes();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.value = reader.bytes();
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
            key: isSet(object.key) ? bytesFromBase64(object.key) : new Uint8Array(0),
            value: isSet(object.value) ? bytesFromBase64(object.value) : new Uint8Array(0)
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.address !== "") {
            obj.address = message.address;
        }
        if (message.key.length !== 0) {
            obj.key = base64FromBytes(message.key);
        }
        if (message.value.length !== 0) {
            obj.value = base64FromBytes(message.value);
        }
        return obj;
    },
    create(base) {
        return ContractState.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseContractState();
        message.address = object.address ?? "";
        message.key = object.key ?? new Uint8Array(0);
        message.value = object.value ?? new Uint8Array(0);
        return message;
    }
};
export const Nonce = {
    $type: "paxprotocol.paxchain.evm.Nonce",
    encode(message, writer = new BinaryWriter()) {
        if (message.address !== "") {
            writer.uint32(10).string(message.address);
        }
        if (message.nonce !== 0) {
            writer.uint32(16).uint64(message.nonce);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseNonce();
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
                    message.nonce = longToNumber(reader.uint64());
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
            nonce: isSet(object.nonce) ? globalThis.Number(object.nonce) : 0
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.address !== "") {
            obj.address = message.address;
        }
        if (message.nonce !== 0) {
            obj.nonce = Math.round(message.nonce);
        }
        return obj;
    },
    create(base) {
        return Nonce.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseNonce();
        message.address = object.address ?? "";
        message.nonce = object.nonce ?? 0;
        return message;
    }
};
export const Serialized = {
    $type: "paxprotocol.paxchain.evm.Serialized",
    encode(message, writer = new BinaryWriter()) {
        if (message.prefix.length !== 0) {
            writer.uint32(10).bytes(message.prefix);
        }
        if (message.key.length !== 0) {
            writer.uint32(18).bytes(message.key);
        }
        if (message.value.length !== 0) {
            writer.uint32(26).bytes(message.value);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseSerialized();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.prefix = reader.bytes();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.key = reader.bytes();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.value = reader.bytes();
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
            prefix: isSet(object.prefix) ? bytesFromBase64(object.prefix) : new Uint8Array(0),
            key: isSet(object.key) ? bytesFromBase64(object.key) : new Uint8Array(0),
            value: isSet(object.value) ? bytesFromBase64(object.value) : new Uint8Array(0)
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.prefix.length !== 0) {
            obj.prefix = base64FromBytes(message.prefix);
        }
        if (message.key.length !== 0) {
            obj.key = base64FromBytes(message.key);
        }
        if (message.value.length !== 0) {
            obj.value = base64FromBytes(message.value);
        }
        return obj;
    },
    create(base) {
        return Serialized.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseSerialized();
        message.prefix = object.prefix ?? new Uint8Array(0);
        message.key = object.key ?? new Uint8Array(0);
        message.value = object.value ?? new Uint8Array(0);
        return message;
    }
};
export const GenesisState = {
    $type: "paxprotocol.paxchain.evm.GenesisState",
    encode(message, writer = new BinaryWriter()) {
        if (message.params !== undefined) {
            Params.encode(message.params, writer.uint32(10).fork()).join();
        }
        for (const v of message.address_associations) {
            AddressAssociation.encode(v, writer.uint32(18).fork()).join();
        }
        for (const v of message.codes) {
            Code.encode(v, writer.uint32(26).fork()).join();
        }
        for (const v of message.states) {
            ContractState.encode(v, writer.uint32(34).fork()).join();
        }
        for (const v of message.nonces) {
            Nonce.encode(v, writer.uint32(42).fork()).join();
        }
        for (const v of message.serialized) {
            Serialized.encode(v, writer.uint32(50).fork()).join();
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
                    message.address_associations.push(AddressAssociation.decode(reader, reader.uint32()));
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.codes.push(Code.decode(reader, reader.uint32()));
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.states.push(ContractState.decode(reader, reader.uint32()));
                    continue;
                case 5:
                    if (tag !== 42) {
                        break;
                    }
                    message.nonces.push(Nonce.decode(reader, reader.uint32()));
                    continue;
                case 6:
                    if (tag !== 50) {
                        break;
                    }
                    message.serialized.push(Serialized.decode(reader, reader.uint32()));
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
            address_associations: globalThis.Array.isArray(object?.address_associations)
                ? object.address_associations.map((e) => AddressAssociation.fromJSON(e))
                : [],
            codes: globalThis.Array.isArray(object?.codes) ? object.codes.map((e) => Code.fromJSON(e)) : [],
            states: globalThis.Array.isArray(object?.states) ? object.states.map((e) => ContractState.fromJSON(e)) : [],
            nonces: globalThis.Array.isArray(object?.nonces) ? object.nonces.map((e) => Nonce.fromJSON(e)) : [],
            serialized: globalThis.Array.isArray(object?.serialized) ? object.serialized.map((e) => Serialized.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.params !== undefined) {
            obj.params = Params.toJSON(message.params);
        }
        if (message.address_associations?.length) {
            obj.address_associations = message.address_associations.map((e) => AddressAssociation.toJSON(e));
        }
        if (message.codes?.length) {
            obj.codes = message.codes.map((e) => Code.toJSON(e));
        }
        if (message.states?.length) {
            obj.states = message.states.map((e) => ContractState.toJSON(e));
        }
        if (message.nonces?.length) {
            obj.nonces = message.nonces.map((e) => Nonce.toJSON(e));
        }
        if (message.serialized?.length) {
            obj.serialized = message.serialized.map((e) => Serialized.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return GenesisState.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGenesisState();
        message.params = object.params !== undefined && object.params !== null ? Params.fromPartial(object.params) : undefined;
        message.address_associations = object.address_associations?.map((e) => AddressAssociation.fromPartial(e)) || [];
        message.codes = object.codes?.map((e) => Code.fromPartial(e)) || [];
        message.states = object.states?.map((e) => ContractState.fromPartial(e)) || [];
        message.nonces = object.nonces?.map((e) => Nonce.fromPartial(e)) || [];
        message.serialized = object.serialized?.map((e) => Serialized.fromPartial(e)) || [];
        return message;
    }
};
function createBaseAddressAssociation() {
    return { pax_address: "", eth_address: "" };
}
function createBaseCode() {
    return { address: "", code: new Uint8Array(0) };
}
function createBaseContractState() {
    return { address: "", key: new Uint8Array(0), value: new Uint8Array(0) };
}
function createBaseNonce() {
    return { address: "", nonce: 0 };
}
function createBaseSerialized() {
    return { prefix: new Uint8Array(0), key: new Uint8Array(0), value: new Uint8Array(0) };
}
function createBaseGenesisState() {
    return { params: undefined, address_associations: [], codes: [], states: [], nonces: [], serialized: [] };
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
export const registry = [
    ["/paxprotocol.paxchain.evm.AddressAssociation", AddressAssociation],
    ["/paxprotocol.paxchain.evm.Code", Code],
    ["/paxprotocol.paxchain.evm.ContractState", ContractState],
    ["/paxprotocol.paxchain.evm.Nonce", Nonce],
    ["/paxprotocol.paxchain.evm.Serialized", Serialized],
    ["/paxprotocol.paxchain.evm.GenesisState", GenesisState]
];
export const aminoConverters = {
    "/paxprotocol.paxchain.evm.AddressAssociation": {
        aminoType: "evm/AddressAssociation",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.evm.Code": {
        aminoType: "evm/Code",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.evm.ContractState": {
        aminoType: "evm/ContractState",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.evm.Nonce": {
        aminoType: "evm/Nonce",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.evm.Serialized": {
        aminoType: "evm/Serialized",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.evm.GenesisState": {
        aminoType: "evm/GenesisState",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
