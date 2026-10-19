import type { GeneratedType } from "@cosmjs/proto-signing";
import type { Log as Log_type, Receipt as Receipt_type } from "../../types/evm";
import type { MessageFns } from "../common";
export interface Log extends Log_type {
}
export interface Receipt extends Receipt_type {
}
export declare const Log: MessageFns<Log, "paxprotocol.paxchain.evm.Log">;
export declare const Receipt: MessageFns<Receipt, "paxprotocol.paxchain.evm.Receipt">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/paxprotocol.paxchain.evm.Log": {
        aminoType: string;
        toAmino: (message: Log) => {
            address: string;
            topics: string[];
            data: Uint8Array;
            index: number;
            synthetic: boolean;
        };
        fromAmino: (object: Log) => {
            address: string;
            topics: string[];
            data: Uint8Array;
            index: number;
            synthetic: boolean;
        };
    };
    "/paxprotocol.paxchain.evm.Receipt": {
        aminoType: string;
        toAmino: (message: Receipt) => {
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
            logs: Log_type[];
            logsBloom: Uint8Array;
        };
        fromAmino: (object: Receipt) => {
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
            logs: Log_type[];
            logsBloom: Uint8Array;
        };
    };
};
