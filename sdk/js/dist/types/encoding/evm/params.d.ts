import type { GeneratedType } from "@cosmjs/proto-signing";
import type { ParamsPreV580 as ParamsPreV580_type, Params as Params_type } from "../../types/evm";
import type { MessageFns } from "../common";
export interface Params extends Params_type {
}
export interface ParamsPreV580 extends ParamsPreV580_type {
}
export declare const Params: MessageFns<Params, "paxprotocol.paxchain.evm.Params">;
export declare const ParamsPreV580: MessageFns<ParamsPreV580, "paxprotocol.paxchain.evm.ParamsPreV580">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/paxprotocol.paxchain.evm.Params": {
        aminoType: string;
        toAmino: (message: Params) => {
            priority_normalizer: string;
            base_fee_per_gas: string;
            minimum_fee_per_gas: string;
            whitelisted_cw_code_hashes_for_delegate_call: Uint8Array[];
            deliver_tx_hook_wasm_gas_limit: number;
            max_dynamic_base_fee_upward_adjustment: string;
            max_dynamic_base_fee_downward_adjustment: string;
            target_gas_used_per_block: number;
            maximum_fee_per_gas: string;
        };
        fromAmino: (object: Params) => {
            priority_normalizer: string;
            base_fee_per_gas: string;
            minimum_fee_per_gas: string;
            whitelisted_cw_code_hashes_for_delegate_call: Uint8Array[];
            deliver_tx_hook_wasm_gas_limit: number;
            max_dynamic_base_fee_upward_adjustment: string;
            max_dynamic_base_fee_downward_adjustment: string;
            target_gas_used_per_block: number;
            maximum_fee_per_gas: string;
        };
    };
    "/paxprotocol.paxchain.evm.ParamsPreV580": {
        aminoType: string;
        toAmino: (message: ParamsPreV580) => {
            priority_normalizer: string;
            base_fee_per_gas: string;
            minimum_fee_per_gas: string;
            whitelisted_cw_code_hashes_for_delegate_call: Uint8Array[];
        };
        fromAmino: (object: ParamsPreV580) => {
            priority_normalizer: string;
            base_fee_per_gas: string;
            minimum_fee_per_gas: string;
            whitelisted_cw_code_hashes_for_delegate_call: Uint8Array[];
        };
    };
};
