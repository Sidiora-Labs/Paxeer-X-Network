import * as fm from "../../fetch";
export class Query {
    static Params(req, initReq) {
        return fm.fetchReq(`/cosmos/cosmos-sdk/accesscontrol/params?${fm.renderURLSearchParams(req, [])}`, {
            ...initReq,
            method: "GET"
        });
    }
    static ResourceDependencyMappingFromMessageKey(req, initReq) {
        return fm.fetchReq(`/cosmos/cosmos-sdk/accesscontrol/resource_dependency_mapping_from_message_key/${req["message_key"]}?${fm.renderURLSearchParams(req, ["message_key"])}`, { ...initReq, method: "GET" });
    }
    static ListResourceDependencyMapping(req, initReq) {
        return fm.fetchReq(`/cosmos/cosmos-sdk/accesscontrol/list_resource_dependency_mapping?${fm.renderURLSearchParams(req, [])}`, { ...initReq, method: "GET" });
    }
    static WasmDependencyMapping(req, initReq) {
        return fm.fetchReq(`/cosmos/cosmos-sdk/accesscontrol/wasm_dependency_mapping/${req["contract_address"]}?${fm.renderURLSearchParams(req, ["contract_address"])}`, { ...initReq, method: "GET" });
    }
    static ListWasmDependencyMapping(req, initReq) {
        return fm.fetchReq(`/cosmos/cosmos-sdk/accesscontrol/list_wasm_dependency_mapping?${fm.renderURLSearchParams(req, [])}`, { ...initReq, method: "GET" });
    }
}
