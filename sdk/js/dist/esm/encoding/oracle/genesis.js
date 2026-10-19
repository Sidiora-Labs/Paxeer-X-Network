import { BinaryReader, BinaryWriter } from "@bufbuild/protobuf/wire";
import { AggregateExchangeRateVote, ExchangeRateTuple, Params, PriceSnapshot, VotePenaltyCounter } from "./oracle";
export const GenesisState = {
    $type: "paxprotocol.paxchain.oracle.GenesisState",
    encode(message, writer = new BinaryWriter()) {
        if (message.params !== undefined) {
            Params.encode(message.params, writer.uint32(10).fork()).join();
        }
        for (const v of message.feeder_delegations) {
            FeederDelegation.encode(v, writer.uint32(18).fork()).join();
        }
        for (const v of message.exchange_rates) {
            ExchangeRateTuple.encode(v, writer.uint32(26).fork()).join();
        }
        for (const v of message.penalty_counters) {
            PenaltyCounter.encode(v, writer.uint32(34).fork()).join();
        }
        for (const v of message.aggregate_exchange_rate_votes) {
            AggregateExchangeRateVote.encode(v, writer.uint32(50).fork()).join();
        }
        for (const v of message.price_snapshots) {
            PriceSnapshot.encode(v, writer.uint32(58).fork()).join();
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
                    message.params = Params.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.feeder_delegations.push(FeederDelegation.decode(reader, reader.uint32()));
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.exchange_rates.push(ExchangeRateTuple.decode(reader, reader.uint32()));
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.penalty_counters.push(PenaltyCounter.decode(reader, reader.uint32()));
                    continue;
                case 6:
                    if (tag !== 50) {
                        break;
                    }
                    message.aggregate_exchange_rate_votes.push(AggregateExchangeRateVote.decode(reader, reader.uint32()));
                    continue;
                case 7:
                    if (tag !== 58) {
                        break;
                    }
                    message.price_snapshots.push(PriceSnapshot.decode(reader, reader.uint32()));
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
            params: isSet(object.params) ? Params.fromJSON(object.params) : undefined,
            feeder_delegations: globalThis.Array.isArray(object?.feeder_delegations) ? object.feeder_delegations.map((e) => FeederDelegation.fromJSON(e)) : [],
            exchange_rates: globalThis.Array.isArray(object?.exchange_rates) ? object.exchange_rates.map((e) => ExchangeRateTuple.fromJSON(e)) : [],
            penalty_counters: globalThis.Array.isArray(object?.penalty_counters) ? object.penalty_counters.map((e) => PenaltyCounter.fromJSON(e)) : [],
            aggregate_exchange_rate_votes: globalThis.Array.isArray(object?.aggregate_exchange_rate_votes)
                ? object.aggregate_exchange_rate_votes.map((e) => AggregateExchangeRateVote.fromJSON(e))
                : [],
            price_snapshots: globalThis.Array.isArray(object?.price_snapshots) ? object.price_snapshots.map((e) => PriceSnapshot.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.params !== undefined) {
            obj.params = Params.toJSON(message.params);
        }
        if (message.feeder_delegations?.length) {
            obj.feeder_delegations = message.feeder_delegations.map((e) => FeederDelegation.toJSON(e));
        }
        if (message.exchange_rates?.length) {
            obj.exchange_rates = message.exchange_rates.map((e) => ExchangeRateTuple.toJSON(e));
        }
        if (message.penalty_counters?.length) {
            obj.penalty_counters = message.penalty_counters.map((e) => PenaltyCounter.toJSON(e));
        }
        if (message.aggregate_exchange_rate_votes?.length) {
            obj.aggregate_exchange_rate_votes = message.aggregate_exchange_rate_votes.map((e) => AggregateExchangeRateVote.toJSON(e));
        }
        if (message.price_snapshots?.length) {
            obj.price_snapshots = message.price_snapshots.map((e) => PriceSnapshot.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return GenesisState.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGenesisState();
        message.params = object.params !== undefined && object.params !== null ? Params.fromPartial(object.params) : undefined;
        message.feeder_delegations = object.feeder_delegations?.map((e) => FeederDelegation.fromPartial(e)) || [];
        message.exchange_rates = object.exchange_rates?.map((e) => ExchangeRateTuple.fromPartial(e)) || [];
        message.penalty_counters = object.penalty_counters?.map((e) => PenaltyCounter.fromPartial(e)) || [];
        message.aggregate_exchange_rate_votes = object.aggregate_exchange_rate_votes?.map((e) => AggregateExchangeRateVote.fromPartial(e)) || [];
        message.price_snapshots = object.price_snapshots?.map((e) => PriceSnapshot.fromPartial(e)) || [];
        return message;
    }
};
export const FeederDelegation = {
    $type: "paxprotocol.paxchain.oracle.FeederDelegation",
    encode(message, writer = new BinaryWriter()) {
        if (message.feeder_address !== "") {
            writer.uint32(10).string(message.feeder_address);
        }
        if (message.validator_address !== "") {
            writer.uint32(18).string(message.validator_address);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
        return FeederDelegation.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseFeederDelegation();
        message.feeder_address = object.feeder_address ?? "";
        message.validator_address = object.validator_address ?? "";
        return message;
    }
};
export const PenaltyCounter = {
    $type: "paxprotocol.paxchain.oracle.PenaltyCounter",
    encode(message, writer = new BinaryWriter()) {
        if (message.validator_address !== "") {
            writer.uint32(10).string(message.validator_address);
        }
        if (message.vote_penalty_counter !== undefined) {
            VotePenaltyCounter.encode(message.vote_penalty_counter, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
                    message.vote_penalty_counter = VotePenaltyCounter.decode(reader, reader.uint32());
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
            vote_penalty_counter: isSet(object.vote_penalty_counter) ? VotePenaltyCounter.fromJSON(object.vote_penalty_counter) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.validator_address !== "") {
            obj.validator_address = message.validator_address;
        }
        if (message.vote_penalty_counter !== undefined) {
            obj.vote_penalty_counter = VotePenaltyCounter.toJSON(message.vote_penalty_counter);
        }
        return obj;
    },
    create(base) {
        return PenaltyCounter.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBasePenaltyCounter();
        message.validator_address = object.validator_address ?? "";
        message.vote_penalty_counter =
            object.vote_penalty_counter !== undefined && object.vote_penalty_counter !== null
                ? VotePenaltyCounter.fromPartial(object.vote_penalty_counter)
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
export const registry = [
    ["/paxprotocol.paxchain.oracle.GenesisState", GenesisState],
    ["/paxprotocol.paxchain.oracle.FeederDelegation", FeederDelegation],
    ["/paxprotocol.paxchain.oracle.PenaltyCounter", PenaltyCounter]
];
export const aminoConverters = {
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
