"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.GenesisDenom = exports.GenesisState = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
const authorityMetadata_1 = require("./authorityMetadata");
const params_1 = require("./params");
exports.GenesisState = {
    $type: "paxprotocol.paxchain.tokenfactory.GenesisState",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.params !== undefined) {
            params_1.Params.encode(message.params, writer.uint32(10).fork()).join();
        }
        for (const v of message.factory_denoms) {
            exports.GenesisDenom.encode(v, writer.uint32(18).fork()).join();
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
                    message.params = params_1.Params.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.factory_denoms.push(exports.GenesisDenom.decode(reader, reader.uint32()));
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
            params: isSet(object.params) ? params_1.Params.fromJSON(object.params) : undefined,
            factory_denoms: globalThis.Array.isArray(object?.factory_denoms) ? object.factory_denoms.map((e) => exports.GenesisDenom.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.params !== undefined) {
            obj.params = params_1.Params.toJSON(message.params);
        }
        if (message.factory_denoms?.length) {
            obj.factory_denoms = message.factory_denoms.map((e) => exports.GenesisDenom.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.GenesisState.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGenesisState();
        message.params = object.params !== undefined && object.params !== null ? params_1.Params.fromPartial(object.params) : undefined;
        message.factory_denoms = object.factory_denoms?.map((e) => exports.GenesisDenom.fromPartial(e)) || [];
        return message;
    }
};
exports.GenesisDenom = {
    $type: "paxprotocol.paxchain.tokenfactory.GenesisDenom",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.denom !== "") {
            writer.uint32(10).string(message.denom);
        }
        if (message.authority_metadata !== undefined) {
            authorityMetadata_1.DenomAuthorityMetadata.encode(message.authority_metadata, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
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
                    message.authority_metadata = authorityMetadata_1.DenomAuthorityMetadata.decode(reader, reader.uint32());
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
            authority_metadata: isSet(object.authority_metadata) ? authorityMetadata_1.DenomAuthorityMetadata.fromJSON(object.authority_metadata) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.denom !== "") {
            obj.denom = message.denom;
        }
        if (message.authority_metadata !== undefined) {
            obj.authority_metadata = authorityMetadata_1.DenomAuthorityMetadata.toJSON(message.authority_metadata);
        }
        return obj;
    },
    create(base) {
        return exports.GenesisDenom.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGenesisDenom();
        message.denom = object.denom ?? "";
        message.authority_metadata =
            object.authority_metadata !== undefined && object.authority_metadata !== null ? authorityMetadata_1.DenomAuthorityMetadata.fromPartial(object.authority_metadata) : undefined;
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
exports.registry = [
    ["/paxprotocol.paxchain.tokenfactory.GenesisState", exports.GenesisState],
    ["/paxprotocol.paxchain.tokenfactory.GenesisDenom", exports.GenesisDenom]
];
exports.aminoConverters = {
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
