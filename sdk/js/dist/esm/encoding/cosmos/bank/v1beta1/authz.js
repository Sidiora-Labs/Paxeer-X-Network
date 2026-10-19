import { BinaryReader, BinaryWriter } from "@bufbuild/protobuf/wire";
import { Coin } from "../../base/v1beta1/coin";
export const SendAuthorization = {
    $type: "cosmos.bank.v1beta1.SendAuthorization",
    encode(message, writer = new BinaryWriter()) {
        for (const v of message.spend_limit) {
            Coin.encode(v, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseSendAuthorization();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.spend_limit.push(Coin.decode(reader, reader.uint32()));
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
            spend_limit: globalThis.Array.isArray(object?.spend_limit) ? object.spend_limit.map((e) => Coin.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.spend_limit?.length) {
            obj.spend_limit = message.spend_limit.map((e) => Coin.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return SendAuthorization.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseSendAuthorization();
        message.spend_limit = object.spend_limit?.map((e) => Coin.fromPartial(e)) || [];
        return message;
    }
};
function createBaseSendAuthorization() {
    return { spend_limit: [] };
}
export const registry = [["/cosmos.bank.v1beta1.SendAuthorization", SendAuthorization]];
export const aminoConverters = {
    "/cosmos.bank.v1beta1.SendAuthorization": {
        aminoType: "cosmos-sdk/SendAuthorization",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
