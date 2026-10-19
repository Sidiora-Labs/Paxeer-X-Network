import { BinaryReader, BinaryWriter } from "@bufbuild/protobuf/wire";
import { Epoch } from "./epoch";
import { Params } from "./params";
export const GenesisState = {
    $type: "paxprotocol.paxchain.epoch.GenesisState",
    encode(message, writer = new BinaryWriter()) {
        if (message.params !== undefined) {
            Params.encode(message.params, writer.uint32(10).fork()).join();
        }
        if (message.epoch !== undefined) {
            Epoch.encode(message.epoch, writer.uint32(18).fork()).join();
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
                    message.epoch = Epoch.decode(reader, reader.uint32());
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
            epoch: isSet(object.epoch) ? Epoch.fromJSON(object.epoch) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.params !== undefined) {
            obj.params = Params.toJSON(message.params);
        }
        if (message.epoch !== undefined) {
            obj.epoch = Epoch.toJSON(message.epoch);
        }
        return obj;
    },
    create(base) {
        return GenesisState.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGenesisState();
        message.params = object.params !== undefined && object.params !== null ? Params.fromPartial(object.params) : undefined;
        message.epoch = object.epoch !== undefined && object.epoch !== null ? Epoch.fromPartial(object.epoch) : undefined;
        return message;
    }
};
function createBaseGenesisState() {
    return { params: undefined, epoch: undefined };
}
function isSet(value) {
    return value !== null && value !== undefined;
}
export const registry = [["/paxprotocol.paxchain.epoch.GenesisState", GenesisState]];
export const aminoConverters = {
    "/paxprotocol.paxchain.epoch.GenesisState": {
        aminoType: "epoch/GenesisState",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
