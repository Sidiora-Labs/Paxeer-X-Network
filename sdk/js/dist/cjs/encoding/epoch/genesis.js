"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.GenesisState = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
const epoch_1 = require("./epoch");
const params_1 = require("./params");
exports.GenesisState = {
    $type: "paxprotocol.paxchain.epoch.GenesisState",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.params !== undefined) {
            params_1.Params.encode(message.params, writer.uint32(10).fork()).join();
        }
        if (message.epoch !== undefined) {
            epoch_1.Epoch.encode(message.epoch, writer.uint32(18).fork()).join();
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
                    message.epoch = epoch_1.Epoch.decode(reader, reader.uint32());
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
            epoch: isSet(object.epoch) ? epoch_1.Epoch.fromJSON(object.epoch) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.params !== undefined) {
            obj.params = params_1.Params.toJSON(message.params);
        }
        if (message.epoch !== undefined) {
            obj.epoch = epoch_1.Epoch.toJSON(message.epoch);
        }
        return obj;
    },
    create(base) {
        return exports.GenesisState.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGenesisState();
        message.params = object.params !== undefined && object.params !== null ? params_1.Params.fromPartial(object.params) : undefined;
        message.epoch = object.epoch !== undefined && object.epoch !== null ? epoch_1.Epoch.fromPartial(object.epoch) : undefined;
        return message;
    }
};
function createBaseGenesisState() {
    return { params: undefined, epoch: undefined };
}
function isSet(value) {
    return value !== null && value !== undefined;
}
exports.registry = [["/paxprotocol.paxchain.epoch.GenesisState", exports.GenesisState]];
exports.aminoConverters = {
    "/paxprotocol.paxchain.epoch.GenesisState": {
        aminoType: "epoch/GenesisState",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
