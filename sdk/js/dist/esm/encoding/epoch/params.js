import { BinaryReader, BinaryWriter } from "@bufbuild/protobuf/wire";
export const Params = {
    $type: "paxprotocol.paxchain.epoch.Params",
    encode(_, writer = new BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseParams();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
            }
            if ((tag & 7) === 4 || tag === 0) {
                break;
            }
            reader.skip(tag & 7);
        }
        return message;
    },
    fromJSON(_) {
        return {};
    },
    toJSON(_) {
        const obj = {};
        return obj;
    },
    create(base) {
        return Params.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseParams();
        return message;
    }
};
function createBaseParams() {
    return {};
}
export const registry = [["/paxprotocol.paxchain.epoch.Params", Params]];
export const aminoConverters = {
    "/paxprotocol.paxchain.epoch.Params": {
        aminoType: "epoch/Params",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
