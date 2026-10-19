import { BinaryReader, BinaryWriter } from "@bufbuild/protobuf/wire";
export const ChainConfig = {
    $type: "paxprotocol.paxchain.evm.ChainConfig",
    encode(message, writer = new BinaryWriter()) {
        if (message.cancun_time !== 0) {
            writer.uint32(8).int64(message.cancun_time);
        }
        if (message.prague_time !== 0) {
            writer.uint32(16).int64(message.prague_time);
        }
        if (message.verkle_time !== 0) {
            writer.uint32(24).int64(message.verkle_time);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseChainConfig();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.cancun_time = longToNumber(reader.int64());
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.prague_time = longToNumber(reader.int64());
                    continue;
                case 3:
                    if (tag !== 24) {
                        break;
                    }
                    message.verkle_time = longToNumber(reader.int64());
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
            cancun_time: isSet(object.cancun_time) ? globalThis.Number(object.cancun_time) : 0,
            prague_time: isSet(object.prague_time) ? globalThis.Number(object.prague_time) : 0,
            verkle_time: isSet(object.verkle_time) ? globalThis.Number(object.verkle_time) : 0
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.cancun_time !== 0) {
            obj.cancun_time = Math.round(message.cancun_time);
        }
        if (message.prague_time !== 0) {
            obj.prague_time = Math.round(message.prague_time);
        }
        if (message.verkle_time !== 0) {
            obj.verkle_time = Math.round(message.verkle_time);
        }
        return obj;
    },
    create(base) {
        return ChainConfig.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseChainConfig();
        message.cancun_time = object.cancun_time ?? 0;
        message.prague_time = object.prague_time ?? 0;
        message.verkle_time = object.verkle_time ?? 0;
        return message;
    }
};
function createBaseChainConfig() {
    return { cancun_time: 0, prague_time: 0, verkle_time: 0 };
}
function longToNumber(int64) {
    const num = globalThis.Number(int64.toString());
    if (num > globalThis.Number.MAX_SAFE_INTEGER) {
        throw new globalThis.Error("Value is larger than Number.MAX_SAFE_INTEGER");
    }
    if (num < globalThis.Number.MIN_SAFE_INTEGER) {
        throw new globalThis.Error("Value is smaller than Number.MIN_SAFE_INTEGER");
    }
    return num;
}
function isSet(value) {
    return value !== null && value !== undefined;
}
export const registry = [["/paxprotocol.paxchain.evm.ChainConfig", ChainConfig]];
export const aminoConverters = {
    "/paxprotocol.paxchain.evm.ChainConfig": {
        aminoType: "evm/ChainConfig",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
