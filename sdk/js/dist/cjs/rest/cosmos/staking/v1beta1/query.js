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
const fm = __importStar(require("../../../fetch"));
class Query {
    static Validators(req, initReq) {
        return fm.fetchReq(`/cosmos/staking/v1beta1/validators?${fm.renderURLSearchParams(req, [])}`, {
            ...initReq,
            method: "GET"
        });
    }
    static Validator(req, initReq) {
        return fm.fetchReq(`/cosmos/staking/v1beta1/validators/${req["validator_addr"]}?${fm.renderURLSearchParams(req, ["validator_addr"])}`, { ...initReq, method: "GET" });
    }
    static ValidatorDelegations(req, initReq) {
        return fm.fetchReq(`/cosmos/staking/v1beta1/validators/${req["validator_addr"]}/delegations?${fm.renderURLSearchParams(req, ["validator_addr"])}`, { ...initReq, method: "GET" });
    }
    static ValidatorUnbondingDelegations(req, initReq) {
        return fm.fetchReq(`/cosmos/staking/v1beta1/validators/${req["validator_addr"]}/unbonding_delegations?${fm.renderURLSearchParams(req, ["validator_addr"])}`, { ...initReq, method: "GET" });
    }
    static Delegation(req, initReq) {
        return fm.fetchReq(`/cosmos/staking/v1beta1/validators/${req["validator_addr"]}/delegations/${req["delegator_addr"]}?${fm.renderURLSearchParams(req, ["validator_addr", "delegator_addr"])}`, { ...initReq, method: "GET" });
    }
    static UnbondingDelegation(req, initReq) {
        return fm.fetchReq(`/cosmos/staking/v1beta1/validators/${req["validator_addr"]}/delegations/${req["delegator_addr"]}/unbonding_delegation?${fm.renderURLSearchParams(req, ["validator_addr", "delegator_addr"])}`, { ...initReq, method: "GET" });
    }
    static DelegatorDelegations(req, initReq) {
        return fm.fetchReq(`/cosmos/staking/v1beta1/delegations/${req["delegator_addr"]}?${fm.renderURLSearchParams(req, ["delegator_addr"])}`, { ...initReq, method: "GET" });
    }
    static DelegatorUnbondingDelegations(req, initReq) {
        return fm.fetchReq(`/cosmos/staking/v1beta1/delegators/${req["delegator_addr"]}/unbonding_delegations?${fm.renderURLSearchParams(req, ["delegator_addr"])}`, { ...initReq, method: "GET" });
    }
    static Redelegations(req, initReq) {
        return fm.fetchReq(`/cosmos/staking/v1beta1/delegators/${req["delegator_addr"]}/redelegations?${fm.renderURLSearchParams(req, ["delegator_addr"])}`, { ...initReq, method: "GET" });
    }
    static DelegatorValidators(req, initReq) {
        return fm.fetchReq(`/cosmos/staking/v1beta1/delegators/${req["delegator_addr"]}/validators?${fm.renderURLSearchParams(req, ["delegator_addr"])}`, { ...initReq, method: "GET" });
    }
    static DelegatorValidator(req, initReq) {
        return fm.fetchReq(`/cosmos/staking/v1beta1/delegators/${req["delegator_addr"]}/validators/${req["validator_addr"]}?${fm.renderURLSearchParams(req, ["delegator_addr", "validator_addr"])}`, { ...initReq, method: "GET" });
    }
    static HistoricalInfo(req, initReq) {
        return fm.fetchReq(`/cosmos/staking/v1beta1/historical_info/${req["height"]}?${fm.renderURLSearchParams(req, ["height"])}`, { ...initReq, method: "GET" });
    }
    static Pool(req, initReq) {
        return fm.fetchReq(`/cosmos/staking/v1beta1/pool?${fm.renderURLSearchParams(req, [])}`, { ...initReq, method: "GET" });
    }
    static Params(req, initReq) {
        return fm.fetchReq(`/cosmos/staking/v1beta1/params?${fm.renderURLSearchParams(req, [])}`, {
            ...initReq,
            method: "GET"
        });
    }
}
exports.Query = Query;
