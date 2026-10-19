import * as fm from "../fetch";
export class Query {
    static ExchangeRate(req, initReq) {
        return fm.fetchReq(`/pax-protocol/pax-chain/oracle/denoms/${req["denom"]}/exchange_rate?${fm.renderURLSearchParams(req, ["denom"])}`, { ...initReq, method: "GET" });
    }
    static ExchangeRates(req, initReq) {
        return fm.fetchReq(`/pax-protocol/pax-chain/oracle/denoms/exchange_rates?${fm.renderURLSearchParams(req, [])}`, { ...initReq, method: "GET" });
    }
    static Actives(req, initReq) {
        return fm.fetchReq(`/pax-protocol/pax-chain/oracle/denoms/actives?${fm.renderURLSearchParams(req, [])}`, {
            ...initReq,
            method: "GET"
        });
    }
    static VoteTargets(req, initReq) {
        return fm.fetchReq(`/pax-protocol/pax-chain/oracle/denoms/vote_targets?${fm.renderURLSearchParams(req, [])}`, { ...initReq, method: "GET" });
    }
    static PriceSnapshotHistory(req, initReq) {
        return fm.fetchReq(`/pax-protocol/pax-chain/oracle/denoms/price_snapshot_history?${fm.renderURLSearchParams(req, [])}`, { ...initReq, method: "GET" });
    }
    static Twaps(req, initReq) {
        return fm.fetchReq(`/pax-protocol/pax-chain/oracle/denoms/twaps/${req["lookback_seconds"]}?${fm.renderURLSearchParams(req, ["lookback_seconds"])}`, { ...initReq, method: "GET" });
    }
    static FeederDelegation(req, initReq) {
        return fm.fetchReq(`/pax-protocol/pax-chain/oracle/validators/${req["validator_addr"]}/feeder?${fm.renderURLSearchParams(req, ["validator_addr"])}`, { ...initReq, method: "GET" });
    }
    static VotePenaltyCounter(req, initReq) {
        return fm.fetchReq(`/pax-protocol/pax-chain/oracle/validators/${req["validator_addr"]}/vote_penalty_counter?${fm.renderURLSearchParams(req, ["validator_addr"])}`, { ...initReq, method: "GET" });
    }
    static SlashWindow(req, initReq) {
        return fm.fetchReq(`/pax-protocol/pax-chain/oracle/slash_window?${fm.renderURLSearchParams(req, [])}`, {
            ...initReq,
            method: "GET"
        });
    }
    static Params(req, initReq) {
        return fm.fetchReq(`/pax-protocol/pax-chain/oracle/params?${fm.renderURLSearchParams(req, [])}`, {
            ...initReq,
            method: "GET"
        });
    }
}
