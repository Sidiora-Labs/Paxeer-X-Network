import { BinaryReader, BinaryWriter } from "@bufbuild/protobuf/wire";
import { Grant } from "./feegrant";
export const GenesisState = {
    $type: "cosmos.feegrant.v1beta1.GenesisState",
    encode(message, writer = new BinaryWriter()) {
        for (const v of message.allowances) {
            Grant.encode(v, writer.uint32(10).fork()).join();
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
                    message.allowances.push(Grant.decode(reader, reader.uint32()));
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
            allowances: globalThis.Array.isArray(object?.allowances) ? object.allowances.map((e) => Grant.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.allowances?.length) {
            obj.allowances = message.allowances.map((e) => Grant.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return GenesisState.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGenesisState();
        message.allowances = object.allowances?.map((e) => Grant.fromPartial(e)) || [];
        return message;
    }
};
function createBaseGenesisState() {
    return { allowances: [] };
}
export const registry = [["/cosmos.feegrant.v1beta1.GenesisState", GenesisState]];
export const aminoConverters = {
    "/cosmos.feegrant.v1beta1.GenesisState": {
        aminoType: "cosmos-sdk/GenesisState",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
