export interface Log {
    address: string;
    topics: string[];
    data: Uint8Array;
    index: number;
    synthetic: boolean;
}
export interface Receipt {
    tx_type: number;
    cumulative_gas_used: number;
    contract_address: string;
    tx_hash_hex: string;
    gas_used: number;
    effective_gas_price: number;
    block_number: number;
    transaction_index: number;
    status: number;
    from: string;
    to: string;
    vm_error: string;
    logs: Log[];
    logsBloom: Uint8Array;
}
