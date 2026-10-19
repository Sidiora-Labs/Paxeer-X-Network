import type { GeneratedType } from "@cosmjs/proto-signing";
import type { FeederDelegation as FeederDelegation_type, GenesisState as GenesisState_type, PenaltyCounter as PenaltyCounter_type } from "../../types/oracle";
import type { MessageFns } from "../common";
export interface GenesisState extends GenesisState_type {
}
export interface FeederDelegation extends FeederDelegation_type {
}
export interface PenaltyCounter extends PenaltyCounter_type {
}
export declare const GenesisState: MessageFns<GenesisState, "paxprotocol.paxchain.oracle.GenesisState">;
export declare const FeederDelegation: MessageFns<FeederDelegation, "paxprotocol.paxchain.oracle.FeederDelegation">;
export declare const PenaltyCounter: MessageFns<PenaltyCounter, "paxprotocol.paxchain.oracle.PenaltyCounter">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/paxprotocol.paxchain.oracle.GenesisState": {
        aminoType: string;
        toAmino: (message: GenesisState) => {
            params?: import("../../types/oracle").Params;
            feeder_delegations: FeederDelegation_type[];
            exchange_rates: import("../../types/oracle").ExchangeRateTuple[];
            penalty_counters: PenaltyCounter_type[];
            aggregate_exchange_rate_votes: import("../../types/oracle").AggregateExchangeRateVote[];
            price_snapshots: import("../../types/oracle").PriceSnapshot[];
        };
        fromAmino: (object: GenesisState) => {
            params?: import("../../types/oracle").Params;
            feeder_delegations: FeederDelegation_type[];
            exchange_rates: import("../../types/oracle").ExchangeRateTuple[];
            penalty_counters: PenaltyCounter_type[];
            aggregate_exchange_rate_votes: import("../../types/oracle").AggregateExchangeRateVote[];
            price_snapshots: import("../../types/oracle").PriceSnapshot[];
        };
    };
    "/paxprotocol.paxchain.oracle.FeederDelegation": {
        aminoType: string;
        toAmino: (message: FeederDelegation) => {
            feeder_address: string;
            validator_address: string;
        };
        fromAmino: (object: FeederDelegation) => {
            feeder_address: string;
            validator_address: string;
        };
    };
    "/paxprotocol.paxchain.oracle.PenaltyCounter": {
        aminoType: string;
        toAmino: (message: PenaltyCounter) => {
            validator_address: string;
            vote_penalty_counter?: import("../../types/oracle").VotePenaltyCounter;
        };
        fromAmino: (object: PenaltyCounter) => {
            validator_address: string;
            vote_penalty_counter?: import("../../types/oracle").VotePenaltyCounter;
        };
    };
};
