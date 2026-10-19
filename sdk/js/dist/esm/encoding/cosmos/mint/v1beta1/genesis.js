import { BinaryReader, BinaryWriter } from "@bufbuild/protobuf/wire";
import { Minter, Params } from "./mint";
export const GenesisState = {
    $type: "cosmos.mint.v1beta1.GenesisState",
    encode(message, writer = new BinaryWriter()) {
        if (message.minter !== undefined) {
            Minter.encode(message.minter, writer.uint32(10).fork()).join();
        }
        if (message.params !== undefined) {
            Params.encode(message.params, writer.uint32(18).fork()).join();
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
                    message.minter = Minter.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.params = Params.decode(reader, reader.uint32());
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
            minter: isSet(object.minter) ? Minter.fromJSON(object.minter) : undefined,
            params: isSet(object.params) ? Params.fromJSON(object.params) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.minter !== undefined) {
            obj.minter = Minter.toJSON(message.minter);
        }
        if (message.params !== undefined) {
            obj.params = Params.toJSON(message.params);
        }
        return obj;
    },
    create(base) {
        return GenesisState.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGenesisState();
        message.minter = object.minter !== undefined && object.minter !== null ? Minter.fromPartial(object.minter) : undefined;
        message.params = object.params !== undefined && object.params !== null ? Params.fromPartial(object.params) : undefined;
        return message;
    }
};
function createBaseGenesisState() {
    return { minter: undefined, params: undefined };
}
function isSet(value) {
    return value !== null && value !== undefined;
}
export const registry = [["/cosmos.mint.v1beta1.GenesisState", GenesisState]];
export const aminoConverters = {
    "/cosmos.mint.v1beta1.GenesisState": {
        aminoType: "cosmos-sdk/GenesisState",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
