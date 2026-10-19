import { BinaryReader, BinaryWriter } from "@bufbuild/protobuf/wire";
export const Params = {
    $type: "paxprotocol.paxchain.oracle.Params",
    encode(message, writer = new BinaryWriter()) {
        if (message.vote_period !== 0) {
            writer.uint32(8).uint64(message.vote_period);
        }
        if (message.vote_threshold !== "") {
            writer.uint32(18).string(message.vote_threshold);
        }
        if (message.reward_band !== "") {
            writer.uint32(26).string(message.reward_band);
        }
        for (const v of message.whitelist) {
            Denom.encode(v, writer.uint32(34).fork()).join();
        }
        if (message.slash_fraction !== "") {
            writer.uint32(42).string(message.slash_fraction);
        }
        if (message.slash_window !== 0) {
            writer.uint32(48).uint64(message.slash_window);
        }
        if (message.min_valid_per_window !== "") {
            writer.uint32(58).string(message.min_valid_per_window);
        }
        if (message.lookback_duration !== 0) {
            writer.uint32(72).uint64(message.lookback_duration);
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
                    message.vote_period = longToNumber(reader.uint64());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.vote_threshold = reader.string();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.reward_band = reader.string();
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.whitelist.push(Denom.decode(reader, reader.uint32()));
                    continue;
                case 5:
                    if (tag !== 42) {
                        break;
                    }
                    message.slash_fraction = reader.string();
                    continue;
                case 6:
                    if (tag !== 48) {
                        break;
                    }
                    message.slash_window = longToNumber(reader.uint64());
                    continue;
                case 7:
                    if (tag !== 58) {
                        break;
                    }
                    message.min_valid_per_window = reader.string();
                    continue;
                case 9:
                    if (tag !== 72) {
                        break;
                    }
                    message.lookback_duration = longToNumber(reader.uint64());
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
            vote_period: isSet(object.vote_period) ? globalThis.Number(object.vote_period) : 0,
            vote_threshold: isSet(object.vote_threshold) ? globalThis.String(object.vote_threshold) : "",
            reward_band: isSet(object.reward_band) ? globalThis.String(object.reward_band) : "",
            whitelist: globalThis.Array.isArray(object?.whitelist) ? object.whitelist.map((e) => Denom.fromJSON(e)) : [],
            slash_fraction: isSet(object.slash_fraction) ? globalThis.String(object.slash_fraction) : "",
            slash_window: isSet(object.slash_window) ? globalThis.Number(object.slash_window) : 0,
            min_valid_per_window: isSet(object.min_valid_per_window) ? globalThis.String(object.min_valid_per_window) : "",
            lookback_duration: isSet(object.lookback_duration) ? globalThis.Number(object.lookback_duration) : 0
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.vote_period !== 0) {
            obj.vote_period = Math.round(message.vote_period);
        }
        if (message.vote_threshold !== "") {
            obj.vote_threshold = message.vote_threshold;
        }
        if (message.reward_band !== "") {
            obj.reward_band = message.reward_band;
        }
        if (message.whitelist?.length) {
            obj.whitelist = message.whitelist.map((e) => Denom.toJSON(e));
        }
        if (message.slash_fraction !== "") {
            obj.slash_fraction = message.slash_fraction;
        }
        if (message.slash_window !== 0) {
            obj.slash_window = Math.round(message.slash_window);
        }
        if (message.min_valid_per_window !== "") {
            obj.min_valid_per_window = message.min_valid_per_window;
        }
        if (message.lookback_duration !== 0) {
            obj.lookback_duration = Math.round(message.lookback_duration);
        }
        return obj;
    },
    create(base) {
        return Params.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseParams();
        message.vote_period = object.vote_period ?? 0;
        message.vote_threshold = object.vote_threshold ?? "";
        message.reward_band = object.reward_band ?? "";
        message.whitelist = object.whitelist?.map((e) => Denom.fromPartial(e)) || [];
        message.slash_fraction = object.slash_fraction ?? "";
        message.slash_window = object.slash_window ?? 0;
        message.min_valid_per_window = object.min_valid_per_window ?? "";
        message.lookback_duration = object.lookback_duration ?? 0;
        return message;
    }
};
export const Denom = {
    $type: "paxprotocol.paxchain.oracle.Denom",
    encode(message, writer = new BinaryWriter()) {
        if (message.name !== "") {
            writer.uint32(10).string(message.name);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseDenom();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.name = reader.string();
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
        return { name: isSet(object.name) ? globalThis.String(object.name) : "" };
    },
    toJSON(message) {
        const obj = {};
        if (message.name !== "") {
            obj.name = message.name;
        }
        return obj;
    },
    create(base) {
        return Denom.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseDenom();
        message.name = object.name ?? "";
        return message;
    }
};
export const AggregateExchangeRateVote = {
    $type: "paxprotocol.paxchain.oracle.AggregateExchangeRateVote",
    encode(message, writer = new BinaryWriter()) {
        for (const v of message.exchange_rate_tuples) {
            ExchangeRateTuple.encode(v, writer.uint32(10).fork()).join();
        }
        if (message.voter !== "") {
            writer.uint32(18).string(message.voter);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseAggregateExchangeRateVote();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.exchange_rate_tuples.push(ExchangeRateTuple.decode(reader, reader.uint32()));
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.voter = reader.string();
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
            exchange_rate_tuples: globalThis.Array.isArray(object?.exchange_rate_tuples)
                ? object.exchange_rate_tuples.map((e) => ExchangeRateTuple.fromJSON(e))
                : [],
            voter: isSet(object.voter) ? globalThis.String(object.voter) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.exchange_rate_tuples?.length) {
            obj.exchange_rate_tuples = message.exchange_rate_tuples.map((e) => ExchangeRateTuple.toJSON(e));
        }
        if (message.voter !== "") {
            obj.voter = message.voter;
        }
        return obj;
    },
    create(base) {
        return AggregateExchangeRateVote.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseAggregateExchangeRateVote();
        message.exchange_rate_tuples = object.exchange_rate_tuples?.map((e) => ExchangeRateTuple.fromPartial(e)) || [];
        message.voter = object.voter ?? "";
        return message;
    }
};
export const ExchangeRateTuple = {
    $type: "paxprotocol.paxchain.oracle.ExchangeRateTuple",
    encode(message, writer = new BinaryWriter()) {
        if (message.denom !== "") {
            writer.uint32(10).string(message.denom);
        }
        if (message.exchange_rate !== "") {
            writer.uint32(18).string(message.exchange_rate);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseExchangeRateTuple();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.denom = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.exchange_rate = reader.string();
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
            denom: isSet(object.denom) ? globalThis.String(object.denom) : "",
            exchange_rate: isSet(object.exchange_rate) ? globalThis.String(object.exchange_rate) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.denom !== "") {
            obj.denom = message.denom;
        }
        if (message.exchange_rate !== "") {
            obj.exchange_rate = message.exchange_rate;
        }
        return obj;
    },
    create(base) {
        return ExchangeRateTuple.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseExchangeRateTuple();
        message.denom = object.denom ?? "";
        message.exchange_rate = object.exchange_rate ?? "";
        return message;
    }
};
export const OracleExchangeRate = {
    $type: "paxprotocol.paxchain.oracle.OracleExchangeRate",
    encode(message, writer = new BinaryWriter()) {
        if (message.exchange_rate !== "") {
            writer.uint32(10).string(message.exchange_rate);
        }
        if (message.last_update !== "") {
            writer.uint32(18).string(message.last_update);
        }
        if (message.last_update_timestamp !== 0) {
            writer.uint32(24).int64(message.last_update_timestamp);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseOracleExchangeRate();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.exchange_rate = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.last_update = reader.string();
                    continue;
                case 3:
                    if (tag !== 24) {
                        break;
                    }
                    message.last_update_timestamp = longToNumber(reader.int64());
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
            exchange_rate: isSet(object.exchange_rate) ? globalThis.String(object.exchange_rate) : "",
            last_update: isSet(object.last_update) ? globalThis.String(object.last_update) : "",
            last_update_timestamp: isSet(object.last_update_timestamp) ? globalThis.Number(object.last_update_timestamp) : 0
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.exchange_rate !== "") {
            obj.exchange_rate = message.exchange_rate;
        }
        if (message.last_update !== "") {
            obj.last_update = message.last_update;
        }
        if (message.last_update_timestamp !== 0) {
            obj.last_update_timestamp = Math.round(message.last_update_timestamp);
        }
        return obj;
    },
    create(base) {
        return OracleExchangeRate.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseOracleExchangeRate();
        message.exchange_rate = object.exchange_rate ?? "";
        message.last_update = object.last_update ?? "";
        message.last_update_timestamp = object.last_update_timestamp ?? 0;
        return message;
    }
};
export const PriceSnapshotItem = {
    $type: "paxprotocol.paxchain.oracle.PriceSnapshotItem",
    encode(message, writer = new BinaryWriter()) {
        if (message.denom !== "") {
            writer.uint32(10).string(message.denom);
        }
        if (message.oracle_exchange_rate !== undefined) {
            OracleExchangeRate.encode(message.oracle_exchange_rate, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBasePriceSnapshotItem();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.denom = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.oracle_exchange_rate = OracleExchangeRate.decode(reader, reader.uint32());
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
            denom: isSet(object.denom) ? globalThis.String(object.denom) : "",
            oracle_exchange_rate: isSet(object.oracle_exchange_rate) ? OracleExchangeRate.fromJSON(object.oracle_exchange_rate) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.denom !== "") {
            obj.denom = message.denom;
        }
        if (message.oracle_exchange_rate !== undefined) {
            obj.oracle_exchange_rate = OracleExchangeRate.toJSON(message.oracle_exchange_rate);
        }
        return obj;
    },
    create(base) {
        return PriceSnapshotItem.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBasePriceSnapshotItem();
        message.denom = object.denom ?? "";
        message.oracle_exchange_rate =
            object.oracle_exchange_rate !== undefined && object.oracle_exchange_rate !== null
                ? OracleExchangeRate.fromPartial(object.oracle_exchange_rate)
                : undefined;
        return message;
    }
};
export const PriceSnapshot = {
    $type: "paxprotocol.paxchain.oracle.PriceSnapshot",
    encode(message, writer = new BinaryWriter()) {
        if (message.snapshot_timestamp !== 0) {
            writer.uint32(8).int64(message.snapshot_timestamp);
        }
        for (const v of message.price_snapshot_items) {
            PriceSnapshotItem.encode(v, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBasePriceSnapshot();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.snapshot_timestamp = longToNumber(reader.int64());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.price_snapshot_items.push(PriceSnapshotItem.decode(reader, reader.uint32()));
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
            snapshot_timestamp: isSet(object.snapshot_timestamp) ? globalThis.Number(object.snapshot_timestamp) : 0,
            price_snapshot_items: globalThis.Array.isArray(object?.price_snapshot_items)
                ? object.price_snapshot_items.map((e) => PriceSnapshotItem.fromJSON(e))
                : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.snapshot_timestamp !== 0) {
            obj.snapshot_timestamp = Math.round(message.snapshot_timestamp);
        }
        if (message.price_snapshot_items?.length) {
            obj.price_snapshot_items = message.price_snapshot_items.map((e) => PriceSnapshotItem.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return PriceSnapshot.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBasePriceSnapshot();
        message.snapshot_timestamp = object.snapshot_timestamp ?? 0;
        message.price_snapshot_items = object.price_snapshot_items?.map((e) => PriceSnapshotItem.fromPartial(e)) || [];
        return message;
    }
};
export const OracleTwap = {
    $type: "paxprotocol.paxchain.oracle.OracleTwap",
    encode(message, writer = new BinaryWriter()) {
        if (message.denom !== "") {
            writer.uint32(10).string(message.denom);
        }
        if (message.twap !== "") {
            writer.uint32(18).string(message.twap);
        }
        if (message.lookback_seconds !== 0) {
            writer.uint32(24).int64(message.lookback_seconds);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseOracleTwap();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.denom = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.twap = reader.string();
                    continue;
                case 3:
                    if (tag !== 24) {
                        break;
                    }
                    message.lookback_seconds = longToNumber(reader.int64());
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
            denom: isSet(object.denom) ? globalThis.String(object.denom) : "",
            twap: isSet(object.twap) ? globalThis.String(object.twap) : "",
            lookback_seconds: isSet(object.lookback_seconds) ? globalThis.Number(object.lookback_seconds) : 0
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.denom !== "") {
            obj.denom = message.denom;
        }
        if (message.twap !== "") {
            obj.twap = message.twap;
        }
        if (message.lookback_seconds !== 0) {
            obj.lookback_seconds = Math.round(message.lookback_seconds);
        }
        return obj;
    },
    create(base) {
        return OracleTwap.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseOracleTwap();
        message.denom = object.denom ?? "";
        message.twap = object.twap ?? "";
        message.lookback_seconds = object.lookback_seconds ?? 0;
        return message;
    }
};
export const VotePenaltyCounter = {
    $type: "paxprotocol.paxchain.oracle.VotePenaltyCounter",
    encode(message, writer = new BinaryWriter()) {
        if (message.miss_count !== 0) {
            writer.uint32(8).uint64(message.miss_count);
        }
        if (message.abstain_count !== 0) {
            writer.uint32(16).uint64(message.abstain_count);
        }
        if (message.success_count !== 0) {
            writer.uint32(24).uint64(message.success_count);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseVotePenaltyCounter();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.miss_count = longToNumber(reader.uint64());
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.abstain_count = longToNumber(reader.uint64());
                    continue;
                case 3:
                    if (tag !== 24) {
                        break;
                    }
                    message.success_count = longToNumber(reader.uint64());
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
            miss_count: isSet(object.miss_count) ? globalThis.Number(object.miss_count) : 0,
            abstain_count: isSet(object.abstain_count) ? globalThis.Number(object.abstain_count) : 0,
            success_count: isSet(object.success_count) ? globalThis.Number(object.success_count) : 0
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.miss_count !== 0) {
            obj.miss_count = Math.round(message.miss_count);
        }
        if (message.abstain_count !== 0) {
            obj.abstain_count = Math.round(message.abstain_count);
        }
        if (message.success_count !== 0) {
            obj.success_count = Math.round(message.success_count);
        }
        return obj;
    },
    create(base) {
        return VotePenaltyCounter.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseVotePenaltyCounter();
        message.miss_count = object.miss_count ?? 0;
        message.abstain_count = object.abstain_count ?? 0;
        message.success_count = object.success_count ?? 0;
        return message;
    }
};
function createBaseParams() {
    return {
        vote_period: 0,
        vote_threshold: "",
        reward_band: "",
        whitelist: [],
        slash_fraction: "",
        slash_window: 0,
        min_valid_per_window: "",
        lookback_duration: 0
    };
}
function createBaseDenom() {
    return { name: "" };
}
function createBaseAggregateExchangeRateVote() {
    return { exchange_rate_tuples: [], voter: "" };
}
function createBaseExchangeRateTuple() {
    return { denom: "", exchange_rate: "" };
}
function createBaseOracleExchangeRate() {
    return { exchange_rate: "", last_update: "", last_update_timestamp: 0 };
}
function createBasePriceSnapshotItem() {
    return { denom: "", oracle_exchange_rate: undefined };
}
function createBasePriceSnapshot() {
    return { snapshot_timestamp: 0, price_snapshot_items: [] };
}
function createBaseOracleTwap() {
    return { denom: "", twap: "", lookback_seconds: 0 };
}
function createBaseVotePenaltyCounter() {
    return { miss_count: 0, abstain_count: 0, success_count: 0 };
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
export const registry = [
    ["/paxprotocol.paxchain.oracle.Params", Params],
    ["/paxprotocol.paxchain.oracle.Denom", Denom],
    ["/paxprotocol.paxchain.oracle.AggregateExchangeRateVote", AggregateExchangeRateVote],
    ["/paxprotocol.paxchain.oracle.ExchangeRateTuple", ExchangeRateTuple],
    ["/paxprotocol.paxchain.oracle.OracleExchangeRate", OracleExchangeRate],
    ["/paxprotocol.paxchain.oracle.PriceSnapshotItem", PriceSnapshotItem],
    ["/paxprotocol.paxchain.oracle.PriceSnapshot", PriceSnapshot],
    ["/paxprotocol.paxchain.oracle.OracleTwap", OracleTwap],
    ["/paxprotocol.paxchain.oracle.VotePenaltyCounter", VotePenaltyCounter]
];
export const aminoConverters = {
    "/paxprotocol.paxchain.oracle.Params": {
        aminoType: "oracle/Params",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.oracle.Denom": {
        aminoType: "oracle/Denom",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.oracle.AggregateExchangeRateVote": {
        aminoType: "oracle/AggregateExchangeRateVote",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.oracle.ExchangeRateTuple": {
        aminoType: "oracle/ExchangeRateTuple",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.oracle.OracleExchangeRate": {
        aminoType: "oracle/OracleExchangeRate",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.oracle.PriceSnapshotItem": {
        aminoType: "oracle/PriceSnapshotItem",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.oracle.PriceSnapshot": {
        aminoType: "oracle/PriceSnapshot",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.oracle.OracleTwap": {
        aminoType: "oracle/OracleTwap",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.oracle.VotePenaltyCounter": {
        aminoType: "oracle/VotePenaltyCounter",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
