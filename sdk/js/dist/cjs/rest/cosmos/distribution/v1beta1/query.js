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
    static Params(req, initReq) {
        return fm.fetchReq(`/cosmos/distribution/v1beta1/params?${fm.renderURLSearchParams(req, [])}`, {
            ...initReq,
            method: "GET"
        });
    }
    static ValidatorOutstandingRewards(req, initReq) {
        return fm.fetchReq(`/cosmos/distribution/v1beta1/validators/${req["validator_address"]}/outstanding_rewards?${fm.renderURLSearchParams(req, ["validator_address"])}`, { ...initReq, method: "GET" });
    }
    static ValidatorCommission(req, initReq) {
        return fm.fetchReq(`/cosmos/distribution/v1beta1/validators/${req["validator_address"]}/commission?${fm.renderURLSearchParams(req, ["validator_address"])}`, { ...initReq, method: "GET" });
    }
    static ValidatorSlashes(req, initReq) {
        return fm.fetchReq(`/cosmos/distribution/v1beta1/validators/${req["validator_address"]}/slashes?${fm.renderURLSearchParams(req, ["validator_address"])}`, { ...initReq, method: "GET" });
    }
    static DelegationRewards(req, initReq) {
        return fm.fetchReq(`/cosmos/distribution/v1beta1/delegators/${req["delegator_address"]}/rewards/${req["validator_address"]}?${fm.renderURLSearchParams(req, ["delegator_address", "validator_address"])}`, { ...initReq, method: "GET" });
    }
    static DelegationTotalRewards(req, initReq) {
        return fm.fetchReq(`/cosmos/distribution/v1beta1/delegators/${req["delegator_address"]}/rewards?${fm.renderURLSearchParams(req, ["delegator_address"])}`, { ...initReq, method: "GET" });
    }
    static DelegatorValidators(req, initReq) {
        return fm.fetchReq(`/cosmos/distribution/v1beta1/delegators/${req["delegator_address"]}/validators?${fm.renderURLSearchParams(req, ["delegator_address"])}`, { ...initReq, method: "GET" });
    }
    static DelegatorWithdrawAddress(req, initReq) {
        return fm.fetchReq(`/cosmos/distribution/v1beta1/delegators/${req["delegator_address"]}/withdraw_address?${fm.renderURLSearchParams(req, ["delegator_address"])}`, { ...initReq, method: "GET" });
    }
    static CommunityPool(req, initReq) {
        return fm.fetchReq(`/cosmos/distribution/v1beta1/community_pool?${fm.renderURLSearchParams(req, [])}`, { ...initReq, method: "GET" });
    }
}
exports.Query = Query;
