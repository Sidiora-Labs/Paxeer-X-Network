"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.GenesisState = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
const coin_1 = require("../../base/v1beta1/coin");
exports.GenesisState = {
    $type: "cosmos.crisis.v1beta1.GenesisState",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.constant_fee !== undefined) {
            coin_1.Coin.encode(message.constant_fee, writer.uint32(26).fork()).join();
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
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.constant_fee = coin_1.Coin.decode(reader, reader.uint32());
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
        return { constant_fee: isSet(object.constant_fee) ? coin_1.Coin.fromJSON(object.constant_fee) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.constant_fee !== undefined) {
            obj.constant_fee = coin_1.Coin.toJSON(message.constant_fee);
        }
        return obj;
    },
    create(base) {
        return exports.GenesisState.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGenesisState();
        message.constant_fee = object.constant_fee !== undefined && object.constant_fee !== null ? coin_1.Coin.fromPartial(object.constant_fee) : undefined;
        return message;
    }
};
function createBaseGenesisState() {
    return { constant_fee: undefined };
}
function isSet(value) {
    return value !== null && value !== undefined;
}
exports.registry = [["/cosmos.crisis.v1beta1.GenesisState", exports.GenesisState]];
exports.aminoConverters = {
    "/cosmos.crisis.v1beta1.GenesisState": {
        aminoType: "cosmos-sdk/GenesisState",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
