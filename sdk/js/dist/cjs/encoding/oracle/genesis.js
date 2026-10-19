"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.PenaltyCounter = exports.FeederDelegation = exports.GenesisState = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
const oracle_1 = require("./oracle");
exports.GenesisState = {
    $type: "paxprotocol.paxchain.oracle.GenesisState",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.params !== undefined) {
            oracle_1.Params.encode(message.params, writer.uint32(10).fork()).join();
        }
        for (const v of message.feeder_delegations) {
            exports.FeederDelegation.encode(v, writer.uint32(18).fork()).join();
        }
        for (const v of message.exchange_rates) {
            oracle_1.ExchangeRateTuple.encode(v, writer.uint32(26).fork()).join();
        }
        for (const v of message.penalty_counters) {
            exports.PenaltyCounter.encode(v, writer.uint32(34).fork()).join();
        }
        for (const v of message.aggregate_exchange_rate_votes) {
            oracle_1.AggregateExchangeRateVote.encode(v, writer.uint32(50).fork()).join();
        }
        for (const v of message.price_snapshots) {
            oracle_1.PriceSnapshot.encode(v, writer.uint32(58).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseGenesisState();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.params = oracle_1.Params.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.feeder_delegations.push(exports.FeederDelegation.decode(reader, reader.uint32()));
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.exchange_rates.push(oracle_1.ExchangeRateTuple.decode(reader, reader.uint32()));
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.penalty_counters.push(exports.PenaltyCounter.decode(reader, reader.uint32()));
                    continue;
                case 6:
                    if (tag !== 50) {
                        break;
                    }
                    message.aggregate_exchange_rate_votes.push(oracle_1.AggregateExchangeRateVote.decode(reader, reader.uint32()));
                    continue;
                case 7:
                    if (tag !== 58) {
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
            params: isSet(object.params) ? oracle_1.Params.fromJSON(object.params) : undefined,
            feeder_delegations: globalThis.Array.isArray(object?.feeder_delegations) ? object.feeder_delegations.map((e) => exports.FeederDelegation.fromJSON(e)) : [],
            exchange_rates: globalThis.Array.isArray(object?.exchange_rates) ? object.exchange_rates.map((e) => oracle_1.ExchangeRateTuple.fromJSON(e)) : [],
            penalty_counters: globalThis.Array.isArray(object?.penalty_counters) ? object.penalty_counters.map((e) => exports.PenaltyCounter.fromJSON(e)) : [],
            aggregate_exchange_rate_votes: globalThis.Array.isArray(object?.aggregate_exchange_rate_votes)
                ? object.aggregate_exchange_rate_votes.map((e) => oracle_1.AggregateExchangeRateVote.fromJSON(e))
                : [],
            price_snapshots: globalThis.Array.isArray(object?.price_snapshots) ? object.price_snapshots.map((e) => oracle_1.PriceSnapshot.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.params !== undefined) {
            obj.params = oracle_1.Params.toJSON(message.params);
        }
        if (message.feeder_delegations?.length) {
            obj.feeder_delegations = message.feeder_delegations.map((e) => exports.FeederDelegation.toJSON(e));
        }
        if (message.exchange_rates?.length) {
            obj.exchange_rates = message.exchange_rates.map((e) => oracle_1.ExchangeRateTuple.toJSON(e));
        }
        if (message.penalty_counters?.length) {
            obj.penalty_counters = message.penalty_counters.map((e) => exports.PenaltyCounter.toJSON(e));
        }
        if (message.aggregate_exchange_rate_votes?.length) {
            obj.aggregate_exchange_rate_votes = message.aggregate_exchange_rate_votes.map((e) => oracle_1.AggregateExchangeRateVote.toJSON(e));
        }
        if (message.price_snapshots?.length) {
            obj.price_snapshots = message.price_snapshots.map((e) => oracle_1.PriceSnapshot.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.GenesisState.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGenesisState();
        message.params = object.params !== undefined && object.params !== null ? oracle_1.Params.fromPartial(object.params) : undefined;
        message.feeder_delegations = object.feeder_delegations?.map((e) => exports.FeederDelegation.fromPartial(e)) || [];
        message.exchange_rates = object.exchange_rates?.map((e) => oracle_1.ExchangeRateTuple.fromPartial(e)) || [];
        message.penalty_counters = object.penalty_counters?.map((e) => exports.PenaltyCounter.fromPartial(e)) || [];
        message.aggregate_exchange_rate_votes = object.aggregate_exchange_rate_votes?.map((e) => oracle_1.AggregateExchangeRateVote.fromPartial(e)) || [];
        message.price_snapshots = object.price_snapshots?.map((e) => oracle_1.PriceSnapshot.fromPartial(e)) || [];
        return message;
    }
};
exports.FeederDelegation = {
    $type: "paxprotocol.paxchain.oracle.FeederDelegation",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.feeder_address !== "") {
            writer.uint32(10).string(message.feeder_address);
        }
        if (message.validator_address !== "") {
            writer.uint32(18).string(message.validator_address);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseFeederDelegation();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.feeder_address = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.validator_address = reader.string();
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
            feeder_address: isSet(object.feeder_address) ? globalThis.String(object.feeder_address) : "",
            validator_address: isSet(object.validator_address) ? globalThis.String(object.validator_address) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.feeder_address !== "") {
            obj.feeder_address = message.feeder_address;
        }
        if (message.validator_address !== "") {
            obj.validator_address = message.validator_address;
        }
        return obj;
    },
    create(base) {
        return exports.FeederDelegation.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseFeederDelegation();
        message.feeder_address = object.feeder_address ?? "";
        message.validator_address = object.validator_address ?? "";
        return message;
    }
};
exports.PenaltyCounter = {
    $type: "paxprotocol.paxchain.oracle.PenaltyCounter",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.validator_address !== "") {
            writer.uint32(10).string(message.validator_address);
        }
        if (message.vote_penalty_counter !== undefined) {
            oracle_1.VotePenaltyCounter.encode(message.vote_penalty_counter, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBasePenaltyCounter();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.validator_address = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
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
            validator_address: isSet(object.validator_address) ? globalThis.String(object.validator_address) : "",
            vote_penalty_counter: isSet(object.vote_penalty_counter) ? oracle_1.VotePenaltyCounter.fromJSON(object.vote_penalty_counter) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.validator_address !== "") {
            obj.validator_address = message.validator_address;
        }
        if (message.vote_penalty_counter !== undefined) {
            obj.vote_penalty_counter = oracle_1.VotePenaltyCounter.toJSON(message.vote_penalty_counter);
        }
        return obj;
    },
    create(base) {
        return exports.PenaltyCounter.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBasePenaltyCounter();
        message.validator_address = object.validator_address ?? "";
        message.vote_penalty_counter =
            object.vote_penalty_counter !== undefined && object.vote_penalty_counter !== null
                ? oracle_1.VotePenaltyCounter.fromPartial(object.vote_penalty_counter)
                : undefined;
        return message;
    }
};
function createBaseGenesisState() {
    return {
        params: undefined,
        feeder_delegations: [],
        exchange_rates: [],
        penalty_counters: [],
        aggregate_exchange_rate_votes: [],
        price_snapshots: []
    };
}
function createBaseFeederDelegation() {
    return { feeder_address: "", validator_address: "" };
}
function createBasePenaltyCounter() {
    return { validator_address: "", vote_penalty_counter: undefined };
}
function isSet(value) {
    return value !== null && value !== undefined;
}
exports.registry = [
    ["/paxprotocol.paxchain.oracle.GenesisState", exports.GenesisState],
    ["/paxprotocol.paxchain.oracle.FeederDelegation", exports.FeederDelegation],
    ["/paxprotocol.paxchain.oracle.PenaltyCounter", exports.PenaltyCounter]
];
exports.aminoConverters = {
    "/paxprotocol.paxchain.oracle.GenesisState": {
        aminoType: "oracle/GenesisState",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.oracle.FeederDelegation": {
        aminoType: "oracle/FeederDelegation",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.oracle.PenaltyCounter": {
        aminoType: "oracle/PenaltyCounter",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
