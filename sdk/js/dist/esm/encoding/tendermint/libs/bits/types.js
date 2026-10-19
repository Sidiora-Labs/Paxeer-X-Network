import { BinaryReader, BinaryWriter } from "@bufbuild/protobuf/wire";
export const BitArray = {
    $type: "tendermint.libs.bits.BitArray",
    encode(message, writer = new BinaryWriter()) {
        if (message.bits !== 0) {
            writer.uint32(8).int64(message.bits);
        }
        writer.uint32(18).fork();
        for (const v of message.elems) {
            writer.uint64(v);
        }
        writer.join();
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseBitArray();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.bits = longToNumber(reader.int64());
                    continue;
                case 2:
                    if (tag === 16) {
                        message.elems.push(longToNumber(reader.uint64()));
                        continue;
                    }
                    if (tag === 18) {
                        const end2 = reader.uint32() + reader.pos;
                        while (reader.pos < end2) {
                            message.elems.push(longToNumber(reader.uint64()));
                        }
                        continue;
                    }
                    break;
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
            bits: isSet(object.bits) ? globalThis.Number(object.bits) : 0,
            elems: globalThis.Array.isArray(object?.elems) ? object.elems.map((e) => globalThis.Number(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.bits !== 0) {
            obj.bits = Math.round(message.bits);
        }
        if (message.elems?.length) {
            obj.elems = message.elems.map((e) => Math.round(e));
        }
        return obj;
    },
    create(base) {
        return BitArray.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseBitArray();
        message.bits = object.bits ?? 0;
        message.elems = object.elems?.map((e) => e) || [];
        return message;
    }
};
function createBaseBitArray() {
    return { bits: 0, elems: [] };
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
export const registry = [["/tendermint.libs.bits.BitArray", BitArray]];
export const aminoConverters = {
    "/tendermint.libs.bits.BitArray": {
        aminoType: "tendermint.libs.bits.BitArray",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
