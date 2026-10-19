import type { GeneratedType } from "@cosmjs/proto-signing";
import type { MsgAggregateExchangeRateVoteResponse as MsgAggregateExchangeRateVoteResponse_type, MsgAggregateExchangeRateVote as MsgAggregateExchangeRateVote_type, MsgDelegateFeedConsentResponse as MsgDelegateFeedConsentResponse_type, MsgDelegateFeedConsent as MsgDelegateFeedConsent_type } from "../../types/oracle";
import type { MessageFns } from "../common";
export interface MsgAggregateExchangeRateVote extends MsgAggregateExchangeRateVote_type {
}
export interface MsgAggregateExchangeRateVoteResponse extends MsgAggregateExchangeRateVoteResponse_type {
}
export interface MsgDelegateFeedConsent extends MsgDelegateFeedConsent_type {
}
export interface MsgDelegateFeedConsentResponse extends MsgDelegateFeedConsentResponse_type {
}
export declare const MsgAggregateExchangeRateVote: MessageFns<MsgAggregateExchangeRateVote, "paxprotocol.paxchain.oracle.MsgAggregateExchangeRateVote">;
export declare const MsgAggregateExchangeRateVoteResponse: MessageFns<MsgAggregateExchangeRateVoteResponse, "paxprotocol.paxchain.oracle.MsgAggregateExchangeRateVoteResponse">;
export declare const MsgDelegateFeedConsent: MessageFns<MsgDelegateFeedConsent, "paxprotocol.paxchain.oracle.MsgDelegateFeedConsent">;
export declare const MsgDelegateFeedConsentResponse: MessageFns<MsgDelegateFeedConsentResponse, "paxprotocol.paxchain.oracle.MsgDelegateFeedConsentResponse">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/paxprotocol.paxchain.oracle.MsgAggregateExchangeRateVote": {
        aminoType: string;
        toAmino: (message: MsgAggregateExchangeRateVote) => {
            exchange_rates: string;
            feeder: string;
            validator: string;
        };
        fromAmino: (object: MsgAggregateExchangeRateVote) => {
            exchange_rates: string;
            feeder: string;
            validator: string;
        };
    };
    "/paxprotocol.paxchain.oracle.MsgAggregateExchangeRateVoteResponse": {
        aminoType: string;
        toAmino: (message: MsgAggregateExchangeRateVoteResponse) => {};
        fromAmino: (object: MsgAggregateExchangeRateVoteResponse) => {};
    };
    "/paxprotocol.paxchain.oracle.MsgDelegateFeedConsent": {
        aminoType: string;
        toAmino: (message: MsgDelegateFeedConsent) => {
            operator: string;
            delegate: string;
        };
        fromAmino: (object: MsgDelegateFeedConsent) => {
            operator: string;
            delegate: string;
        };
    };
    "/paxprotocol.paxchain.oracle.MsgDelegateFeedConsentResponse": {
        aminoType: string;
        toAmino: (message: MsgDelegateFeedConsentResponse) => {};
        fromAmino: (object: MsgDelegateFeedConsentResponse) => {};
    };
};
