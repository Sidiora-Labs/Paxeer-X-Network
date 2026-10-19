"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.QueryParamsResponse = exports.QueryParamsRequest = exports.QuerySlashWindowResponse = exports.QuerySlashWindowRequest = exports.QueryVotePenaltyCounterResponse = exports.QueryVotePenaltyCounterRequest = exports.QueryFeederDelegationResponse = exports.QueryFeederDelegationRequest = exports.QueryTwapsResponse = exports.QueryTwapsRequest = exports.QueryPriceSnapshotHistoryResponse = exports.QueryPriceSnapshotHistoryRequest = exports.QueryVoteTargetsResponse = exports.QueryVoteTargetsRequest = exports.QueryActivesResponse = exports.QueryActivesRequest = exports.QueryExchangeRatesResponse = exports.DenomOracleExchangeRatePair = exports.QueryExchangeRatesRequest = exports.QueryExchangeRateResponse = exports.QueryExchangeRateRequest = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
const oracle_1 = require("./oracle");
exports.QueryExchangeRateRequest = {
    $type: "paxprotocol.paxchain.oracle.QueryExchangeRateRequest",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.denom !== "") {
            writer.uint32(10).string(message.denom);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryExchangeRateRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.denom = reader.string();
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
        return { denom: isSet(object.denom) ? globalThis.String(object.denom) : "" };
    },
    toJSON(message) {
        const obj = {};
        if (message.denom !== "") {
            obj.denom = message.denom;
        }
        return obj;
    },
    create(base) {
        return exports.QueryExchangeRateRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryExchangeRateRequest();
        message.denom = object.denom ?? "";
        return message;
    }
};
exports.QueryExchangeRateResponse = {
    $type: "paxprotocol.paxchain.oracle.QueryExchangeRateResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.oracle_exchange_rate !== undefined) {
            oracle_1.OracleExchangeRate.encode(message.oracle_exchange_rate, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryExchangeRateResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.oracle_exchange_rate = oracle_1.OracleExchangeRate.decode(reader, reader.uint32());
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
            oracle_exchange_rate: isSet(object.oracle_exchange_rate) ? oracle_1.OracleExchangeRate.fromJSON(object.oracle_exchange_rate) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.oracle_exchange_rate !== undefined) {
            obj.oracle_exchange_rate = oracle_1.OracleExchangeRate.toJSON(message.oracle_exchange_rate);
        }
        return obj;
    },
    create(base) {
        return exports.QueryExchangeRateResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryExchangeRateResponse();
        message.oracle_exchange_rate =
            object.oracle_exchange_rate !== undefined && object.oracle_exchange_rate !== null
                ? oracle_1.OracleExchangeRate.fromPartial(object.oracle_exchange_rate)
                : undefined;
        return message;
    }
};
exports.QueryExchangeRatesRequest = {
    $type: "paxprotocol.paxchain.oracle.QueryExchangeRatesRequest",
    encode(_, writer = new wire_1.BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryExchangeRatesRequest();
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
        return exports.QueryExchangeRatesRequest.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseQueryExchangeRatesRequest();
        return message;
    }
};
exports.DenomOracleExchangeRatePair = {
    $type: "paxprotocol.paxchain.oracle.DenomOracleExchangeRatePair",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.denom !== "") {
            writer.uint32(10).string(message.denom);
        }
        if (message.oracle_exchange_rate !== undefined) {
            oracle_1.OracleExchangeRate.encode(message.oracle_exchange_rate, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseDenomOracleExchangeRatePair();
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
                    message.oracle_exchange_rate = oracle_1.OracleExchangeRate.decode(reader, reader.uint32());
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
            oracle_exchange_rate: isSet(object.oracle_exchange_rate) ? oracle_1.OracleExchangeRate.fromJSON(object.oracle_exchange_rate) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.denom !== "") {
            obj.denom = message.denom;
        }
        if (message.oracle_exchange_rate !== undefined) {
            obj.oracle_exchange_rate = oracle_1.OracleExchangeRate.toJSON(message.oracle_exchange_rate);
        }
        return obj;
    },
    create(base) {
        return exports.DenomOracleExchangeRatePair.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseDenomOracleExchangeRatePair();
        message.denom = object.denom ?? "";
        message.oracle_exchange_rate =
            object.oracle_exchange_rate !== undefined && object.oracle_exchange_rate !== null
                ? oracle_1.OracleExchangeRate.fromPartial(object.oracle_exchange_rate)
                : undefined;
        return message;
    }
};
exports.QueryExchangeRatesResponse = {
    $type: "paxprotocol.paxchain.oracle.QueryExchangeRatesResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.denom_oracle_exchange_rate_pairs) {
            exports.DenomOracleExchangeRatePair.encode(v, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryExchangeRatesResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.denom_oracle_exchange_rate_pairs.push(exports.DenomOracleExchangeRatePair.decode(reader, reader.uint32()));
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
            denom_oracle_exchange_rate_pairs: globalThis.Array.isArray(object?.denom_oracle_exchange_rate_pairs)
                ? object.denom_oracle_exchange_rate_pairs.map((e) => exports.DenomOracleExchangeRatePair.fromJSON(e))
                : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.denom_oracle_exchange_rate_pairs?.length) {
            obj.denom_oracle_exchange_rate_pairs = message.denom_oracle_exchange_rate_pairs.map((e) => exports.DenomOracleExchangeRatePair.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.QueryExchangeRatesResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryExchangeRatesResponse();
        message.denom_oracle_exchange_rate_pairs = object.denom_oracle_exchange_rate_pairs?.map((e) => exports.DenomOracleExchangeRatePair.fromPartial(e)) || [];
        return message;
    }
};
exports.QueryActivesRequest = {
    $type: "paxprotocol.paxchain.oracle.QueryActivesRequest",
    encode(_, writer = new wire_1.BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryActivesRequest();
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
        return exports.QueryActivesRequest.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseQueryActivesRequest();
        return message;
    }
};
exports.QueryActivesResponse = {
    $type: "paxprotocol.paxchain.oracle.QueryActivesResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.actives) {
            writer.uint32(10).string(v);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryActivesResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.actives.push(reader.string());
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
            actives: globalThis.Array.isArray(object?.actives) ? object.actives.map((e) => globalThis.String(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.actives?.length) {
            obj.actives = message.actives;
        }
        return obj;
    },
    create(base) {
        return exports.QueryActivesResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryActivesResponse();
        message.actives = object.actives?.map((e) => e) || [];
        return message;
    }
};
exports.QueryVoteTargetsRequest = {
    $type: "paxprotocol.paxchain.oracle.QueryVoteTargetsRequest",
    encode(_, writer = new wire_1.BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryVoteTargetsRequest();
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
        return exports.QueryVoteTargetsRequest.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseQueryVoteTargetsRequest();
        return message;
    }
};
exports.QueryVoteTargetsResponse = {
    $type: "paxprotocol.paxchain.oracle.QueryVoteTargetsResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.vote_targets) {
            writer.uint32(10).string(v);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryVoteTargetsResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.vote_targets.push(reader.string());
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
            vote_targets: globalThis.Array.isArray(object?.vote_targets) ? object.vote_targets.map((e) => globalThis.String(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.vote_targets?.length) {
            obj.vote_targets = message.vote_targets;
        }
        return obj;
    },
    create(base) {
        return exports.QueryVoteTargetsResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryVoteTargetsResponse();
        message.vote_targets = object.vote_targets?.map((e) => e) || [];
        return message;
    }
};
exports.QueryPriceSnapshotHistoryRequest = {
    $type: "paxprotocol.paxchain.oracle.QueryPriceSnapshotHistoryRequest",
    encode(_, writer = new wire_1.BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryPriceSnapshotHistoryRequest();
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
        return exports.QueryPriceSnapshotHistoryRequest.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseQueryPriceSnapshotHistoryRequest();
        return message;
    }
};
exports.QueryPriceSnapshotHistoryResponse = {
    $type: "paxprotocol.paxchain.oracle.QueryPriceSnapshotHistoryResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.price_snapshots) {
            oracle_1.PriceSnapshot.encode(v, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryPriceSnapshotHistoryResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.price_snapshots.push(oracle_1.PriceSnapshot.decode(reader, reader.uint32()));
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
            price_snapshots: globalThis.Array.isArray(object?.price_snapshots) ? object.price_snapshots.map((e) => oracle_1.PriceSnapshot.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.price_snapshots?.length) {
            obj.price_snapshots = message.price_snapshots.map((e) => oracle_1.PriceSnapshot.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.QueryPriceSnapshotHistoryResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryPriceSnapshotHistoryResponse();
        message.price_snapshots = object.price_snapshots?.map((e) => oracle_1.PriceSnapshot.fromPartial(e)) || [];
        return message;
    }
};
exports.QueryTwapsRequest = {
    $type: "paxprotocol.paxchain.oracle.QueryTwapsRequest",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.lookback_seconds !== 0) {
            writer.uint32(8).uint64(message.lookback_seconds);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryTwapsRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.lookback_seconds = longToNumber(reader.uint64());
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
        return { lookback_seconds: isSet(object.lookback_seconds) ? globalThis.Number(object.lookback_seconds) : 0 };
    },
    toJSON(message) {
        const obj = {};
        if (message.lookback_seconds !== 0) {
            obj.lookback_seconds = Math.round(message.lookback_seconds);
        }
        return obj;
    },
    create(base) {
        return exports.QueryTwapsRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryTwapsRequest();
        message.lookback_seconds = object.lookback_seconds ?? 0;
        return message;
    }
};
exports.QueryTwapsResponse = {
    $type: "paxprotocol.paxchain.oracle.QueryTwapsResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.oracle_twaps) {
            oracle_1.OracleTwap.encode(v, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryTwapsResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.oracle_twaps.push(oracle_1.OracleTwap.decode(reader, reader.uint32()));
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
            oracle_twaps: globalThis.Array.isArray(object?.oracle_twaps) ? object.oracle_twaps.map((e) => oracle_1.OracleTwap.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.oracle_twaps?.length) {
            obj.oracle_twaps = message.oracle_twaps.map((e) => oracle_1.OracleTwap.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.QueryTwapsResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryTwapsResponse();
        message.oracle_twaps = object.oracle_twaps?.map((e) => oracle_1.OracleTwap.fromPartial(e)) || [];
        return message;
    }
};
exports.QueryFeederDelegationRequest = {
    $type: "paxprotocol.paxchain.oracle.QueryFeederDelegationRequest",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.validator_addr !== "") {
            writer.uint32(10).string(message.validator_addr);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryFeederDelegationRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.validator_addr = reader.string();
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
        return { validator_addr: isSet(object.validator_addr) ? globalThis.String(object.validator_addr) : "" };
    },
    toJSON(message) {
        const obj = {};
        if (message.validator_addr !== "") {
            obj.validator_addr = message.validator_addr;
        }
        return obj;
    },
    create(base) {
        return exports.QueryFeederDelegationRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryFeederDelegationRequest();
        message.validator_addr = object.validator_addr ?? "";
        return message;
    }
};
exports.QueryFeederDelegationResponse = {
    $type: "paxprotocol.paxchain.oracle.QueryFeederDelegationResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.feeder_addr !== "") {
            writer.uint32(10).string(message.feeder_addr);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryFeederDelegationResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.feeder_addr = reader.string();
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
        return { feeder_addr: isSet(object.feeder_addr) ? globalThis.String(object.feeder_addr) : "" };
    },
    toJSON(message) {
        const obj = {};
        if (message.feeder_addr !== "") {
            obj.feeder_addr = message.feeder_addr;
        }
        return obj;
    },
    create(base) {
        return exports.QueryFeederDelegationResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryFeederDelegationResponse();
        message.feeder_addr = object.feeder_addr ?? "";
        return message;
    }
};
exports.QueryVotePenaltyCounterRequest = {
    $type: "paxprotocol.paxchain.oracle.QueryVotePenaltyCounterRequest",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.validator_addr !== "") {
            writer.uint32(10).string(message.validator_addr);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryVotePenaltyCounterRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.validator_addr = reader.string();
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
        return { validator_addr: isSet(object.validator_addr) ? globalThis.String(object.validator_addr) : "" };
    },
    toJSON(message) {
        const obj = {};
        if (message.validator_addr !== "") {
            obj.validator_addr = message.validator_addr;
        }
        return obj;
    },
    create(base) {
        return exports.QueryVotePenaltyCounterRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryVotePenaltyCounterRequest();
        message.validator_addr = object.validator_addr ?? "";
        return message;
    }
};
exports.QueryVotePenaltyCounterResponse = {
    $type: "paxprotocol.paxchain.oracle.QueryVotePenaltyCounterResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.vote_penalty_counter !== undefined) {
            oracle_1.VotePenaltyCounter.encode(message.vote_penalty_counter, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryVotePenaltyCounterResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.vote_penalty_counter = oracle_1.VotePenaltyCounter.decode(reader, reader.uint32());
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
            vote_penalty_counter: isSet(object.vote_penalty_counter) ? oracle_1.VotePenaltyCounter.fromJSON(object.vote_penalty_counter) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.vote_penalty_counter !== undefined) {
            obj.vote_penalty_counter = oracle_1.VotePenaltyCounter.toJSON(message.vote_penalty_counter);
        }
        return obj;
    },
    create(base) {
        return exports.QueryVotePenaltyCounterResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryVotePenaltyCounterResponse();
        message.vote_penalty_counter =
            object.vote_penalty_counter !== undefined && object.vote_penalty_counter !== null
                ? oracle_1.VotePenaltyCounter.fromPartial(object.vote_penalty_counter)
                : undefined;
        return message;
    }
};
exports.QuerySlashWindowRequest = {
    $type: "paxprotocol.paxchain.oracle.QuerySlashWindowRequest",
    encode(_, writer = new wire_1.BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQuerySlashWindowRequest();
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
        return exports.QuerySlashWindowRequest.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseQuerySlashWindowRequest();
        return message;
    }
};
exports.QuerySlashWindowResponse = {
    $type: "paxprotocol.paxchain.oracle.QuerySlashWindowResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.window_progress !== 0) {
            writer.uint32(8).uint64(message.window_progress);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQuerySlashWindowResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.window_progress = longToNumber(reader.uint64());
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
        return { window_progress: isSet(object.window_progress) ? globalThis.Number(object.window_progress) : 0 };
    },
    toJSON(message) {
        const obj = {};
        if (message.window_progress !== 0) {
            obj.window_progress = Math.round(message.window_progress);
        }
        return obj;
    },
    create(base) {
        return exports.QuerySlashWindowResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQuerySlashWindowResponse();
        message.window_progress = object.window_progress ?? 0;
        return message;
    }
};
exports.QueryParamsRequest = {
    $type: "paxprotocol.paxchain.oracle.QueryParamsRequest",
    encode(_, writer = new wire_1.BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryParamsRequest();
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
        return exports.QueryParamsRequest.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseQueryParamsRequest();
        return message;
    }
};
exports.QueryParamsResponse = {
    $type: "paxprotocol.paxchain.oracle.QueryParamsResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.params !== undefined) {
            oracle_1.Params.encode(message.params, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryParamsResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.params = oracle_1.Params.decode(reader, reader.uint32());
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
        return { params: isSet(object.params) ? oracle_1.Params.fromJSON(object.params) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.params !== undefined) {
            obj.params = oracle_1.Params.toJSON(message.params);
        }
        return obj;
    },
    create(base) {
        return exports.QueryParamsResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryParamsResponse();
        message.params = object.params !== undefined && object.params !== null ? oracle_1.Params.fromPartial(object.params) : undefined;
        return message;
    }
};
function createBaseQueryExchangeRateRequest() {
    return { denom: "" };
}
function createBaseQueryExchangeRateResponse() {
    return { oracle_exchange_rate: undefined };
}
function createBaseQueryExchangeRatesRequest() {
    return {};
}
function createBaseDenomOracleExchangeRatePair() {
    return { denom: "", oracle_exchange_rate: undefined };
}
function createBaseQueryExchangeRatesResponse() {
    return { denom_oracle_exchange_rate_pairs: [] };
}
function createBaseQueryActivesRequest() {
    return {};
}
function createBaseQueryActivesResponse() {
    return { actives: [] };
}
function createBaseQueryVoteTargetsRequest() {
    return {};
}
function createBaseQueryVoteTargetsResponse() {
    return { vote_targets: [] };
}
function createBaseQueryPriceSnapshotHistoryRequest() {
    return {};
}
function createBaseQueryPriceSnapshotHistoryResponse() {
    return { price_snapshots: [] };
}
function createBaseQueryTwapsRequest() {
    return { lookback_seconds: 0 };
}
function createBaseQueryTwapsResponse() {
    return { oracle_twaps: [] };
}
function createBaseQueryFeederDelegationRequest() {
    return { validator_addr: "" };
}
function createBaseQueryFeederDelegationResponse() {
    return { feeder_addr: "" };
}
function createBaseQueryVotePenaltyCounterRequest() {
    return { validator_addr: "" };
}
function createBaseQueryVotePenaltyCounterResponse() {
    return { vote_penalty_counter: undefined };
}
function createBaseQuerySlashWindowRequest() {
    return {};
}
function createBaseQuerySlashWindowResponse() {
    return { window_progress: 0 };
}
function createBaseQueryParamsRequest() {
    return {};
}
function createBaseQueryParamsResponse() {
    return { params: undefined };
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
exports.registry = [
    ["/paxprotocol.paxchain.oracle.QueryExchangeRateRequest", exports.QueryExchangeRateRequest],
    ["/paxprotocol.paxchain.oracle.QueryExchangeRateResponse", exports.QueryExchangeRateResponse],
    ["/paxprotocol.paxchain.oracle.QueryExchangeRatesRequest", exports.QueryExchangeRatesRequest],
    ["/paxprotocol.paxchain.oracle.DenomOracleExchangeRatePair", exports.DenomOracleExchangeRatePair],
    ["/paxprotocol.paxchain.oracle.QueryExchangeRatesResponse", exports.QueryExchangeRatesResponse],
    ["/paxprotocol.paxchain.oracle.QueryActivesRequest", exports.QueryActivesRequest],
    ["/paxprotocol.paxchain.oracle.QueryActivesResponse", exports.QueryActivesResponse],
    ["/paxprotocol.paxchain.oracle.QueryVoteTargetsRequest", exports.QueryVoteTargetsRequest],
    ["/paxprotocol.paxchain.oracle.QueryVoteTargetsResponse", exports.QueryVoteTargetsResponse],
    ["/paxprotocol.paxchain.oracle.QueryPriceSnapshotHistoryRequest", exports.QueryPriceSnapshotHistoryRequest],
    ["/paxprotocol.paxchain.oracle.QueryPriceSnapshotHistoryResponse", exports.QueryPriceSnapshotHistoryResponse],
    ["/paxprotocol.paxchain.oracle.QueryTwapsRequest", exports.QueryTwapsRequest],
    ["/paxprotocol.paxchain.oracle.QueryTwapsResponse", exports.QueryTwapsResponse],
    ["/paxprotocol.paxchain.oracle.QueryFeederDelegationRequest", exports.QueryFeederDelegationRequest],
    ["/paxprotocol.paxchain.oracle.QueryFeederDelegationResponse", exports.QueryFeederDelegationResponse],
    ["/paxprotocol.paxchain.oracle.QueryVotePenaltyCounterRequest", exports.QueryVotePenaltyCounterRequest],
    ["/paxprotocol.paxchain.oracle.QueryVotePenaltyCounterResponse", exports.QueryVotePenaltyCounterResponse],
    ["/paxprotocol.paxchain.oracle.QuerySlashWindowRequest", exports.QuerySlashWindowRequest],
    ["/paxprotocol.paxchain.oracle.QuerySlashWindowResponse", exports.QuerySlashWindowResponse],
    ["/paxprotocol.paxchain.oracle.QueryParamsRequest", exports.QueryParamsRequest],
    ["/paxprotocol.paxchain.oracle.QueryParamsResponse", exports.QueryParamsResponse]
];
exports.aminoConverters = {
    "/paxprotocol.paxchain.oracle.QueryExchangeRateRequest": {
        aminoType: "oracle/QueryExchangeRateRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.oracle.QueryExchangeRateResponse": {
        aminoType: "oracle/QueryExchangeRateResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.oracle.QueryExchangeRatesRequest": {
        aminoType: "oracle/QueryExchangeRatesRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.oracle.DenomOracleExchangeRatePair": {
        aminoType: "oracle/DenomOracleExchangeRatePair",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.oracle.QueryExchangeRatesResponse": {
        aminoType: "oracle/QueryExchangeRatesResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.oracle.QueryActivesRequest": {
        aminoType: "oracle/QueryActivesRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.oracle.QueryActivesResponse": {
        aminoType: "oracle/QueryActivesResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.oracle.QueryVoteTargetsRequest": {
        aminoType: "oracle/QueryVoteTargetsRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.oracle.QueryVoteTargetsResponse": {
        aminoType: "oracle/QueryVoteTargetsResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.oracle.QueryPriceSnapshotHistoryRequest": {
        aminoType: "oracle/QueryPriceSnapshotHistoryRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.oracle.QueryPriceSnapshotHistoryResponse": {
        aminoType: "oracle/QueryPriceSnapshotHistoryResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.oracle.QueryTwapsRequest": {
        aminoType: "oracle/QueryTwapsRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.oracle.QueryTwapsResponse": {
        aminoType: "oracle/QueryTwapsResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.oracle.QueryFeederDelegationRequest": {
        aminoType: "oracle/QueryFeederDelegationRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.oracle.QueryFeederDelegationResponse": {
        aminoType: "oracle/QueryFeederDelegationResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.oracle.QueryVotePenaltyCounterRequest": {
        aminoType: "oracle/QueryVotePenaltyCounterRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.oracle.QueryVotePenaltyCounterResponse": {
        aminoType: "oracle/QueryVotePenaltyCounterResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.oracle.QuerySlashWindowRequest": {
        aminoType: "oracle/QuerySlashWindowRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.oracle.QuerySlashWindowResponse": {
        aminoType: "oracle/QuerySlashWindowResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.oracle.QueryParamsRequest": {
        aminoType: "oracle/QueryParamsRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.oracle.QueryParamsResponse": {
        aminoType: "oracle/QueryParamsResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
