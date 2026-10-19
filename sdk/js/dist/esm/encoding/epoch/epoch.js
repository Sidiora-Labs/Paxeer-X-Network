import { BinaryReader, BinaryWriter } from "@bufbuild/protobuf/wire";
import { Duration } from "../google/protobuf/duration";
import { Timestamp } from "../google/protobuf/timestamp";
export const Epoch = {
    $type: "paxprotocol.paxchain.epoch.Epoch",
    encode(message, writer = new BinaryWriter()) {
        if (message.genesis_time !== undefined) {
            Timestamp.encode(toTimestamp(message.genesis_time), writer.uint32(10).fork()).join();
        }
        if (message.epoch_duration !== undefined) {
            Duration.encode(message.epoch_duration, writer.uint32(18).fork()).join();
        }
        if (message.current_epoch !== 0) {
            writer.uint32(24).uint64(message.current_epoch);
        }
        if (message.current_epoch_start_time !== undefined) {
            Timestamp.encode(toTimestamp(message.current_epoch_start_time), writer.uint32(34).fork()).join();
        }
        if (message.current_epoch_height !== 0) {
            writer.uint32(40).int64(message.current_epoch_height);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseEpoch();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.genesis_time = fromTimestamp(Timestamp.decode(reader, reader.uint32()));
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.epoch_duration = Duration.decode(reader, reader.uint32());
                    continue;
                case 3:
                    if (tag !== 24) {
                        break;
                    }
                    message.current_epoch = longToNumber(reader.uint64());
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.current_epoch_start_time = fromTimestamp(Timestamp.decode(reader, reader.uint32()));
                    continue;
                case 5:
                    if (tag !== 40) {
                        break;
                    }
                    message.current_epoch_height = longToNumber(reader.int64());
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
            genesis_time: isSet(object.genesis_time) ? fromJsonTimestamp(object.genesis_time) : undefined,
            epoch_duration: isSet(object.epoch_duration) ? Duration.fromJSON(object.epoch_duration) : undefined,
            current_epoch: isSet(object.current_epoch) ? globalThis.Number(object.current_epoch) : 0,
            current_epoch_start_time: isSet(object.current_epoch_start_time) ? fromJsonTimestamp(object.current_epoch_start_time) : undefined,
            current_epoch_height: isSet(object.current_epoch_height) ? globalThis.Number(object.current_epoch_height) : 0
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.genesis_time !== undefined) {
            obj.genesis_time = message.genesis_time.toISOString();
        }
        if (message.epoch_duration !== undefined) {
            obj.epoch_duration = Duration.toJSON(message.epoch_duration);
        }
        if (message.current_epoch !== 0) {
            obj.current_epoch = Math.round(message.current_epoch);
        }
        if (message.current_epoch_start_time !== undefined) {
            obj.current_epoch_start_time = message.current_epoch_start_time.toISOString();
        }
        if (message.current_epoch_height !== 0) {
            obj.current_epoch_height = Math.round(message.current_epoch_height);
        }
        return obj;
    },
    create(base) {
        return Epoch.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseEpoch();
        message.genesis_time = object.genesis_time ?? undefined;
        message.epoch_duration = object.epoch_duration !== undefined && object.epoch_duration !== null ? Duration.fromPartial(object.epoch_duration) : undefined;
        message.current_epoch = object.current_epoch ?? 0;
        message.current_epoch_start_time = object.current_epoch_start_time ?? undefined;
        message.current_epoch_height = object.current_epoch_height ?? 0;
        return message;
    }
};
function createBaseEpoch() {
    return {
        genesis_time: undefined,
        epoch_duration: undefined,
        current_epoch: 0,
        current_epoch_start_time: undefined,
        current_epoch_height: 0
    };
}
function toTimestamp(date) {
    const seconds = Math.trunc(date.getTime() / 1000);
    const nanos = (date.getTime() % 1000) * 1000000;
    return { seconds, nanos };
}
function fromTimestamp(t) {
    let millis = (t.seconds || 0) * 1000;
    millis += (t.nanos || 0) / 1000000;
    return new globalThis.Date(millis);
}
function fromJsonTimestamp(o) {
    if (o instanceof globalThis.Date) {
        return o;
    }
    else if (typeof o === "string") {
        return new globalThis.Date(o);
    }
    else {
        return fromTimestamp(Timestamp.fromJSON(o));
    }
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
export const registry = [["/paxprotocol.paxchain.epoch.Epoch", Epoch]];
export const aminoConverters = {
    "/paxprotocol.paxchain.epoch.Epoch": {
        aminoType: "epoch/Epoch",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
