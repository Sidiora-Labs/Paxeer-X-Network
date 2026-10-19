import { BinaryReader, BinaryWriter } from "@bufbuild/protobuf/wire";
export const Params = {
    $type: "paxprotocol.paxchain.tokenfactory.Params",
    encode(message, writer = new BinaryWriter()) {
        if (message.denom_allowlist_max_size !== 0) {
            writer.uint32(8).uint32(message.denom_allowlist_max_size);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseParams();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.denom_allowlist_max_size = reader.uint32();
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
            denom_allowlist_max_size: isSet(object.denom_allowlist_max_size) ? globalThis.Number(object.denom_allowlist_max_size) : 0
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.denom_allowlist_max_size !== 0) {
            obj.denom_allowlist_max_size = Math.round(message.denom_allowlist_max_size);
        }
        return obj;
    },
    create(base) {
        return Params.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseParams();
        message.denom_allowlist_max_size = object.denom_allowlist_max_size ?? 0;
        return message;
    }
};
function createBaseParams() {
    return { denom_allowlist_max_size: 0 };
}
function isSet(value) {
    return value !== null && value !== undefined;
}
export const registry = [["/paxprotocol.paxchain.tokenfactory.Params", Params]];
export const aminoConverters = {
    "/paxprotocol.paxchain.tokenfactory.Params": {
        aminoType: "tokenfactory/Params",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
