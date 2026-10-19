import type { GeneratedType } from "@cosmjs/proto-signing";
import type { AggregateExchangeRateVote as AggregateExchangeRateVote_type, Denom as Denom_type, ExchangeRateTuple as ExchangeRateTuple_type, OracleExchangeRate as OracleExchangeRate_type, OracleTwap as OracleTwap_type, Params as Params_type, PriceSnapshotItem as PriceSnapshotItem_type, PriceSnapshot as PriceSnapshot_type, VotePenaltyCounter as VotePenaltyCounter_type } from "../../types/oracle";
import type { MessageFns } from "../common";
export interface Params extends Params_type {
}
export interface Denom extends Denom_type {
}
export interface AggregateExchangeRateVote extends AggregateExchangeRateVote_type {
}
export interface ExchangeRateTuple extends ExchangeRateTuple_type {
}
export interface OracleExchangeRate extends OracleExchangeRate_type {
}
export interface PriceSnapshotItem extends PriceSnapshotItem_type {
}
export interface PriceSnapshot extends PriceSnapshot_type {
}
export interface OracleTwap extends OracleTwap_type {
}
export interface VotePenaltyCounter extends VotePenaltyCounter_type {
}
export declare const Params: MessageFns<Params, "paxprotocol.paxchain.oracle.Params">;
export declare const Denom: MessageFns<Denom, "paxprotocol.paxchain.oracle.Denom">;
export declare const AggregateExchangeRateVote: MessageFns<AggregateExchangeRateVote, "paxprotocol.paxchain.oracle.AggregateExchangeRateVote">;
export declare const ExchangeRateTuple: MessageFns<ExchangeRateTuple, "paxprotocol.paxchain.oracle.ExchangeRateTuple">;
export declare const OracleExchangeRate: MessageFns<OracleExchangeRate, "paxprotocol.paxchain.oracle.OracleExchangeRate">;
export declare const PriceSnapshotItem: MessageFns<PriceSnapshotItem, "paxprotocol.paxchain.oracle.PriceSnapshotItem">;
export declare const PriceSnapshot: MessageFns<PriceSnapshot, "paxprotocol.paxchain.oracle.PriceSnapshot">;
export declare const OracleTwap: MessageFns<OracleTwap, "paxprotocol.paxchain.oracle.OracleTwap">;
export declare const VotePenaltyCounter: MessageFns<VotePenaltyCounter, "paxprotocol.paxchain.oracle.VotePenaltyCounter">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/paxprotocol.paxchain.oracle.Params": {
        aminoType: string;
        toAmino: (message: Params) => {
            vote_period: number;
            vote_threshold: string;
            reward_band: string;
            whitelist: Denom_type[];
            slash_fraction: string;
            slash_window: number;
            min_valid_per_window: string;
            lookback_duration: number;
        };
        fromAmino: (object: Params) => {
            vote_period: number;
            vote_threshold: string;
            reward_band: string;
            whitelist: Denom_type[];
            slash_fraction: string;
            slash_window: number;
            min_valid_per_window: string;
            lookback_duration: number;
        };
    };
    "/paxprotocol.paxchain.oracle.Denom": {
        aminoType: string;
        toAmino: (message: Denom) => {
            name: string;
        };
        fromAmino: (object: Denom) => {
            name: string;
        };
    };
    "/paxprotocol.paxchain.oracle.AggregateExchangeRateVote": {
        aminoType: string;
        toAmino: (message: AggregateExchangeRateVote) => {
            exchange_rate_tuples: ExchangeRateTuple_type[];
            voter: string;
        };
        fromAmino: (object: AggregateExchangeRateVote) => {
            exchange_rate_tuples: ExchangeRateTuple_type[];
            voter: string;
        };
    };
    "/paxprotocol.paxchain.oracle.ExchangeRateTuple": {
        aminoType: string;
        toAmino: (message: ExchangeRateTuple) => {
            denom: string;
            exchange_rate: string;
        };
        fromAmino: (object: ExchangeRateTuple) => {
            denom: string;
            exchange_rate: string;
        };
    };
    "/paxprotocol.paxchain.oracle.OracleExchangeRate": {
        aminoType: string;
        toAmino: (message: OracleExchangeRate) => {
            exchange_rate: string;
            last_update: string;
            last_update_timestamp: number;
        };
        fromAmino: (object: OracleExchangeRate) => {
            exchange_rate: string;
            last_update: string;
            last_update_timestamp: number;
        };
    };
    "/paxprotocol.paxchain.oracle.PriceSnapshotItem": {
        aminoType: string;
        toAmino: (message: PriceSnapshotItem) => {
            denom: string;
            oracle_exchange_rate?: OracleExchangeRate_type;
        };
        fromAmino: (object: PriceSnapshotItem) => {
            denom: string;
            oracle_exchange_rate?: OracleExchangeRate_type;
        };
    };
    "/paxprotocol.paxchain.oracle.PriceSnapshot": {
        aminoType: string;
        toAmino: (message: PriceSnapshot) => {
            snapshot_timestamp: number;
            price_snapshot_items: PriceSnapshotItem_type[];
        };
        fromAmino: (object: PriceSnapshot) => {
            snapshot_timestamp: number;
            price_snapshot_items: PriceSnapshotItem_type[];
        };
    };
    "/paxprotocol.paxchain.oracle.OracleTwap": {
        aminoType: string;
        toAmino: (message: OracleTwap) => {
            denom: string;
            twap: string;
            lookback_seconds: number;
        };
        fromAmino: (object: OracleTwap) => {
            denom: string;
            twap: string;
            lookback_seconds: number;
        };
    };
    "/paxprotocol.paxchain.oracle.VotePenaltyCounter": {
        aminoType: string;
        toAmino: (message: VotePenaltyCounter) => {
            miss_count: number;
            abstain_count: number;
            success_count: number;
        };
        fromAmino: (object: VotePenaltyCounter) => {
            miss_count: number;
            abstain_count: number;
            success_count: number;
        };
    };
};
