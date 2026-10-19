import type { DecCoin } from "../../base/v1beta1/coin";
export interface FeesParams {
    global_minimum_gas_prices: DecCoin[];
    allowed_fee_denoms: string[];
}
export interface CosmosGasParams {
    cosmos_gas_multiplier_numerator: number;
    cosmos_gas_multiplier_denominator: number;
}
export interface GenesisState {
    fees_params?: FeesParams;
    cosmos_gas_params?: CosmosGasParams;
}
