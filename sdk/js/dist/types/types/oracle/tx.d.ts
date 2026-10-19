export interface MsgAggregateExchangeRateVote {
    /** 1 reserved from old field `salt` */
    exchange_rates: string;
    feeder: string;
    validator: string;
}
export type MsgAggregateExchangeRateVoteResponse = {};
export interface MsgDelegateFeedConsent {
    operator: string;
    delegate: string;
}
export type MsgDelegateFeedConsentResponse = {};
