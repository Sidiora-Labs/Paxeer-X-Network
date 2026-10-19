export interface Params {
    /**
     * string base_denom = 1 [
     *   (gogoproto.moretags)   = "yaml:\"base_denom\"",
     *   (gogoproto.jsontag) = "base_denom"
     * ];
     */
    priority_normalizer: string;
    base_fee_per_gas: string;
    minimum_fee_per_gas: string;
    /**
     * ChainConfig chain_config = 5 [(gogoproto.moretags) = "yaml:\"chain_config\"", (gogoproto.nullable) = false];
     *   string chain_id = 6 [
     *   (gogoproto.moretags)   = "yaml:\"chain_id\"",
     *   (gogoproto.customtype) = "github.com/cosmos/cosmos-sdk/types.Int",
     *   (gogoproto.nullable)   = false,
     *   (gogoproto.jsontag) = "chain_id"
     * ];
     * repeated string whitelisted_codehashes_bank_send = 7 [
     *   (gogoproto.moretags)   = "yaml:\"whitelisted_codehashes_bank_send\"",
     *   (gogoproto.jsontag) = "whitelisted_codehashes_bank_send"
     * ];
     */
    whitelisted_cw_code_hashes_for_delegate_call: Uint8Array[];
    deliver_tx_hook_wasm_gas_limit: number;
    max_dynamic_base_fee_upward_adjustment: string;
    max_dynamic_base_fee_downward_adjustment: string;
    target_gas_used_per_block: number;
    maximum_fee_per_gas: string;
}
export interface ParamsPreV580 {
    /**
     * string base_denom = 1 [
     *   (gogoproto.moretags)   = "yaml:\"base_denom\"",
     *   (gogoproto.jsontag) = "base_denom"
     * ];
     */
    priority_normalizer: string;
    base_fee_per_gas: string;
    minimum_fee_per_gas: string;
    /**
     * ChainConfig chain_config = 5 [(gogoproto.moretags) = "yaml:\"chain_config\"", (gogoproto.nullable) = false];
     *   string chain_id = 6 [
     *   (gogoproto.moretags)   = "yaml:\"chain_id\"",
     *   (gogoproto.customtype) = "github.com/cosmos/cosmos-sdk/types.Int",
     *   (gogoproto.nullable)   = false,
     *   (gogoproto.jsontag) = "chain_id"
     * ];
     * repeated string whitelisted_codehashes_bank_send = 7 [
     *   (gogoproto.moretags)   = "yaml:\"whitelisted_codehashes_bank_send\"",
     *   (gogoproto.jsontag) = "whitelisted_codehashes_bank_send"
     * ];
     */
    whitelisted_cw_code_hashes_for_delegate_call: Uint8Array[];
}
