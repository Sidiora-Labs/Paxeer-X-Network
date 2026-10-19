import { BinaryReader, BinaryWriter } from "@bufbuild/protobuf/wire";
import { DenomAuthorityMetadata } from "./authorityMetadata";
import { Params } from "./params";
export const GenesisState = {
    $type: "paxprotocol.paxchain.tokenfactory.GenesisState",
    encode(message, writer = new BinaryWriter()) {
        if (message.params !== undefined) {
            Params.encode(message.params, writer.uint32(10).fork()).join();
        }
        for (const v of message.factory_denoms) {
            GenesisDenom.encode(v, writer.uint32(18).fork()).join();
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
                    message.factory_denoms.push(GenesisDenom.decode(reader, reader.uint32()));
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
            factory_denoms: globalThis.Array.isArray(object?.factory_denoms) ? object.factory_denoms.map((e) => GenesisDenom.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.params !== undefined) {
            obj.params = Params.toJSON(message.params);
        }
        if (message.factory_denoms?.length) {
            obj.factory_denoms = message.factory_denoms.map((e) => GenesisDenom.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return GenesisState.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGenesisState();
        message.params = object.params !== undefined && object.params !== null ? Params.fromPartial(object.params) : undefined;
        message.factory_denoms = object.factory_denoms?.map((e) => GenesisDenom.fromPartial(e)) || [];
        return message;
    }
};
export const GenesisDenom = {
    $type: "paxprotocol.paxchain.tokenfactory.GenesisDenom",
    encode(message, writer = new BinaryWriter()) {
        if (message.denom !== "") {
            writer.uint32(10).string(message.denom);
        }
        if (message.authority_metadata !== undefined) {
            DenomAuthorityMetadata.encode(message.authority_metadata, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseGenesisDenom();
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
                    if (tag !== 18) {
                        break;
                    }
                    message.authority_metadata = DenomAuthorityMetadata.decode(reader, reader.uint32());
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
            authority_metadata: isSet(object.authority_metadata) ? DenomAuthorityMetadata.fromJSON(object.authority_metadata) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.denom !== "") {
            obj.denom = message.denom;
        }
        if (message.authority_metadata !== undefined) {
            obj.authority_metadata = DenomAuthorityMetadata.toJSON(message.authority_metadata);
        }
        return obj;
    },
    create(base) {
        return GenesisDenom.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGenesisDenom();
        message.denom = object.denom ?? "";
        message.authority_metadata =
            object.authority_metadata !== undefined && object.authority_metadata !== null ? DenomAuthorityMetadata.fromPartial(object.authority_metadata) : undefined;
        return message;
    }
};
function createBaseGenesisState() {
    return { params: undefined, factory_denoms: [] };
}
function createBaseGenesisDenom() {
    return { denom: "", authority_metadata: undefined };
}
function isSet(value) {
    return value !== null && value !== undefined;
}
export const registry = [
    ["/paxprotocol.paxchain.tokenfactory.GenesisState", GenesisState],
    ["/paxprotocol.paxchain.tokenfactory.GenesisDenom", GenesisDenom]
];
export const aminoConverters = {
    "/paxprotocol.paxchain.tokenfactory.GenesisState": {
        aminoType: "tokenfactory/GenesisState",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.tokenfactory.GenesisDenom": {
        aminoType: "tokenfactory/GenesisDenom",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
