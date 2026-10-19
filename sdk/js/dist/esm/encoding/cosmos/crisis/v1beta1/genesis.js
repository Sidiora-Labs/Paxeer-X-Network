import { BinaryReader, BinaryWriter } from "@bufbuild/protobuf/wire";
import { Coin } from "../../base/v1beta1/coin";
export const GenesisState = {
    $type: "cosmos.crisis.v1beta1.GenesisState",
    encode(message, writer = new BinaryWriter()) {
        if (message.constant_fee !== undefined) {
            Coin.encode(message.constant_fee, writer.uint32(26).fork()).join();
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
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.constant_fee = Coin.decode(reader, reader.uint32());
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
        return { constant_fee: isSet(object.constant_fee) ? Coin.fromJSON(object.constant_fee) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.constant_fee !== undefined) {
            obj.constant_fee = Coin.toJSON(message.constant_fee);
        }
        return obj;
    },
    create(base) {
        return GenesisState.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGenesisState();
        message.constant_fee = object.constant_fee !== undefined && object.constant_fee !== null ? Coin.fromPartial(object.constant_fee) : undefined;
        return message;
    }
};
function createBaseGenesisState() {
    return { constant_fee: undefined };
}
function isSet(value) {
    return value !== null && value !== undefined;
}
export const registry = [["/cosmos.crisis.v1beta1.GenesisState", GenesisState]];
export const aminoConverters = {
    "/cosmos.crisis.v1beta1.GenesisState": {
        aminoType: "cosmos-sdk/GenesisState",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
