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
    static PaxAddressByEVMAddress(req, initReq) {
        return fm.fetchReq(`/pax-protocol/paxchain/evm/pax_address?${fm.renderURLSearchParams(req, [])}`, { ...initReq, method: "GET" });
    }
    static EVMAddressByPaxAddress(req, initReq) {
        return fm.fetchReq(`/pax-protocol/paxchain/evm/evm_address?${fm.renderURLSearchParams(req, [])}`, { ...initReq, method: "GET" });
    }
    static StaticCall(req, initReq) {
        return fm.fetchReq(`/pax-protocol/paxchain/evm/static_call?${fm.renderURLSearchParams(req, [])}`, {
            ...initReq,
            method: "GET"
        });
    }
    static Pointer(req, initReq) {
        return fm.fetchReq(`/pax-protocol/paxchain/evm/pointer?${fm.renderURLSearchParams(req, [])}`, {
            ...initReq,
            method: "GET"
        });
    }
    static PointerVersion(req, initReq) {
        return fm.fetchReq(`/pax-protocol/paxchain/evm/pointer_version?${fm.renderURLSearchParams(req, [])}`, { ...initReq, method: "GET" });
    }
    static Pointee(req, initReq) {
        return fm.fetchReq(`/pax-protocol/paxchain/evm/pointee?${fm.renderURLSearchParams(req, [])}`, {
            ...initReq,
            method: "GET"
        });
    }
}
exports.Query = Query;
