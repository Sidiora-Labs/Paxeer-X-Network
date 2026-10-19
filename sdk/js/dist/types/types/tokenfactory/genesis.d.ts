import type { DenomAuthorityMetadata } from "./authorityMetadata";
import type { Params } from "./params";
export interface GenesisState {
    /** params defines the paramaters of the module. */
    params?: Params;
    factory_denoms: GenesisDenom[];
}
export interface GenesisDenom {
    denom: string;
    authority_metadata?: DenomAuthorityMetadata;
}
