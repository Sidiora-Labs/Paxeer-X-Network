"use strict";
var __createBinding = (this && this.__createBinding) || (Object.create ? (function(o, m, k, k2) {
    if (k2 === undefined) k2 = k;
    var desc = Object.getOwnPropertyDescriptor(m, k);
    if (!desc || ("get" in desc ? !m.__esModule : desc.writable || desc.configurable)) {
      desc = { enumerable: true, get: function() { return m[k]; } };
    }
    Object.defineProperty(o, k2, desc);
}) : (function(o, m, k, k2) {
    if (k2 === undefined) k2 = k;
    o[k2] = m[k];
}));
var __setModuleDefault = (this && this.__setModuleDefault) || (Object.create ? (function(o, v) {
    Object.defineProperty(o, "default", { enumerable: true, value: v });
}) : function(o, v) {
    o["default"] = v;
});
var __importStar = (this && this.__importStar) || (function () {
    var ownKeys = function(o) {
        ownKeys = Object.getOwnPropertyNames || function (o) {
            var ar = [];
            for (var k in o) if (Object.prototype.hasOwnProperty.call(o, k)) ar[ar.length] = k;
            return ar;
        };
        return ownKeys(o);
    };
    return function (mod) {
        if (mod && mod.__esModule) return mod;
        var result = {};
        if (mod != null) for (var k = ownKeys(mod), i = 0; i < k.length; i++) if (k[i] !== "default") __createBinding(result, mod, k[i]);
        __setModuleDefault(result, mod);
        return result;
    };
})();
Object.defineProperty(exports, "__esModule", { value: true });
exports.Query = void 0;
const fm = __importStar(require("../fetch"));
class Query {
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
exports.Query = Query;
