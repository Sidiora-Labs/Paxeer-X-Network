import type { Any } from "../../../google/protobuf/any";
export interface BaseAccount {
    address: string;
    pub_key?: Any;
    account_number: number;
    sequence: number;
}
export interface ModuleAccount {
    base_account?: BaseAccount;
    name: string;
    permissions: string[];
}
export interface Params {
    max_memo_characters: number;
    tx_sig_limit: number;
    tx_size_cost_per_byte: number;
    sig_verify_cost_ed25519: number;
    sig_verify_cost_secp256k1: number;
    disable_seqno_check: boolean;
}
