import type { Coin } from "../../base/v1beta1/coin";
export interface Params {
    send_enabled: SendEnabled[];
    default_send_enabled: boolean;
}
export interface SendEnabled {
    denom: string;
    enabled: boolean;
}
export interface Input {
    address: string;
    coins: Coin[];
}
export interface Output {
    address: string;
    coins: Coin[];
}
export interface Supply {
    total: Coin[];
}
export interface DenomUnit {
    /** denom represents the string name of the given denom unit (e.g uatom). */
    denom: string;
    /**
     * exponent represents power of 10 exponent that one must
     * raise the base_denom to in order to equal the given DenomUnit's denom
     * 1 denom = 1^exponent base_denom
     * (e.g. with a base_denom of uatom, one can create a DenomUnit of 'atom' with
     * exponent = 6, thus: 1 atom = 10^6 uatom).
     */
    exponent: number;
    /** aliases is a list of string aliases for the given denom */
    aliases: string[];
}
export interface Metadata {
    description: string;
    /** denom_units represents the list of DenomUnit's for a given coin */
    denom_units: DenomUnit[];
    /** base represents the base denom (should be the DenomUnit with exponent = 0). */
    base: string;
    /**
     * display indicates the suggested denom that should be
     * displayed in clients.
     */
    display: string;
    /**
     * name defines the name of the token (eg: Cosmos Atom)
     *
     * Since: cosmos-sdk 0.43
     */
    name: string;
    /**
     * symbol is the token symbol usually shown on exchanges (eg: ATOM). This can
     * be the same as the display.
     *
     * Since: cosmos-sdk 0.43
     */
    symbol: string;
}
export interface AllowList {
    /** Can be empty for no admin, or a valid pax address */
    addresses: string[];
}
