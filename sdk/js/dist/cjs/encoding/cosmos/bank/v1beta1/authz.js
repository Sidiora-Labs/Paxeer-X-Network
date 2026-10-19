"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.SendAuthorization = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
const coin_1 = require("../../base/v1beta1/coin");
exports.SendAuthorization = {
    $type: "cosmos.bank.v1beta1.SendAuthorization",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.spend_limit) {
            coin_1.Coin.encode(v, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseSendAuthorization();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.spend_limit.push(coin_1.Coin.decode(reader, reader.uint32()));
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
            spend_limit: globalThis.Array.isArray(object?.spend_limit) ? object.spend_limit.map((e) => coin_1.Coin.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.spend_limit?.length) {
            obj.spend_limit = message.spend_limit.map((e) => coin_1.Coin.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.SendAuthorization.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseSendAuthorization();
        message.spend_limit = object.spend_limit?.map((e) => coin_1.Coin.fromPartial(e)) || [];
        return message;
    }
};
function createBaseSendAuthorization() {
    return { spend_limit: [] };
}
exports.registry = [["/cosmos.bank.v1beta1.SendAuthorization", exports.SendAuthorization]];
exports.aminoConverters = {
    "/cosmos.bank.v1beta1.SendAuthorization": {
        aminoType: "cosmos-sdk/SendAuthorization",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
