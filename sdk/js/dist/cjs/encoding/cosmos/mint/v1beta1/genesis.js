"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.GenesisState = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
const mint_1 = require("./mint");
exports.GenesisState = {
    $type: "cosmos.mint.v1beta1.GenesisState",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.minter !== undefined) {
            mint_1.Minter.encode(message.minter, writer.uint32(10).fork()).join();
        }
        if (message.params !== undefined) {
            mint_1.Params.encode(message.params, writer.uint32(18).fork()).join();
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
                    message.minter = mint_1.Minter.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.params = mint_1.Params.decode(reader, reader.uint32());
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
            minter: isSet(object.minter) ? mint_1.Minter.fromJSON(object.minter) : undefined,
            params: isSet(object.params) ? mint_1.Params.fromJSON(object.params) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.minter !== undefined) {
            obj.minter = mint_1.Minter.toJSON(message.minter);
        }
        if (message.params !== undefined) {
            obj.params = mint_1.Params.toJSON(message.params);
        }
        return obj;
    },
    create(base) {
        return exports.GenesisState.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGenesisState();
        message.minter = object.minter !== undefined && object.minter !== null ? mint_1.Minter.fromPartial(object.minter) : undefined;
        message.params = object.params !== undefined && object.params !== null ? mint_1.Params.fromPartial(object.params) : undefined;
        return message;
    }
};
function createBaseGenesisState() {
    return { minter: undefined, params: undefined };
}
function isSet(value) {
    return value !== null && value !== undefined;
}
exports.registry = [["/cosmos.mint.v1beta1.GenesisState", exports.GenesisState]];
exports.aminoConverters = {
    "/cosmos.mint.v1beta1.GenesisState": {
        aminoType: "cosmos-sdk/GenesisState",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
