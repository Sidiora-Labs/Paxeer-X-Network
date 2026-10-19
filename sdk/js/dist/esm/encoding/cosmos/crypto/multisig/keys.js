import { BinaryReader, BinaryWriter } from "@bufbuild/protobuf/wire";
import { Any } from "../../../google/protobuf/any";
export const LegacyAminoPubKey = {
    $type: "cosmos.crypto.multisig.LegacyAminoPubKey",
    encode(message, writer = new BinaryWriter()) {
        if (message.threshold !== 0) {
            writer.uint32(8).uint32(message.threshold);
        }
        for (const v of message.public_keys) {
            Any.encode(v, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseLegacyAminoPubKey();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.threshold = reader.uint32();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.public_keys.push(Any.decode(reader, reader.uint32()));
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
            threshold: isSet(object.threshold) ? globalThis.Number(object.threshold) : 0,
            public_keys: globalThis.Array.isArray(object?.public_keys) ? object.public_keys.map((e) => Any.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.threshold !== 0) {
            obj.threshold = Math.round(message.threshold);
        }
        if (message.public_keys?.length) {
            obj.public_keys = message.public_keys.map((e) => Any.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return LegacyAminoPubKey.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseLegacyAminoPubKey();
        message.threshold = object.threshold ?? 0;
        message.public_keys = object.public_keys?.map((e) => Any.fromPartial(e)) || [];
        return message;
    }
};
function createBaseLegacyAminoPubKey() {
    return { threshold: 0, public_keys: [] };
}
function isSet(value) {
    return value !== null && value !== undefined;
}
export const registry = [["/cosmos.crypto.multisig.LegacyAminoPubKey", LegacyAminoPubKey]];
export const aminoConverters = {
    "/cosmos.crypto.multisig.LegacyAminoPubKey": {
        aminoType: "cosmos-sdk/LegacyAminoPubKey",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
