import * as fm from "../fetch";
export class Query {
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
