import type { GeneratedType } from "@cosmjs/proto-signing";
import type { AccessListTx as AccessListTx_type, AccessTuple as AccessTuple_type, AssociateTx as AssociateTx_type, BlobTxSidecar as BlobTxSidecar_type, BlobTx as BlobTx_type, DynamicFeeTx as DynamicFeeTx_type, ExtensionOptionsEthereumTx as ExtensionOptionsEthereumTx_type, LegacyTx as LegacyTx_type } from "../../types/eth";
import type { MessageFns } from "../common";
export interface AccessTuple extends AccessTuple_type {
}
export interface AssociateTx extends AssociateTx_type {
}
export interface LegacyTx extends LegacyTx_type {
}
export interface AccessListTx extends AccessListTx_type {
}
export interface DynamicFeeTx extends DynamicFeeTx_type {
}
export interface BlobTx extends BlobTx_type {
}
export interface BlobTxSidecar extends BlobTxSidecar_type {
}
export interface ExtensionOptionsEthereumTx extends ExtensionOptionsEthereumTx_type {
}
export declare const AccessTuple: MessageFns<AccessTuple, "paxprotocol.paxchain.eth.AccessTuple">;
export declare const AssociateTx: MessageFns<AssociateTx, "paxprotocol.paxchain.eth.AssociateTx">;
export declare const LegacyTx: MessageFns<LegacyTx, "paxprotocol.paxchain.eth.LegacyTx">;
export declare const AccessListTx: MessageFns<AccessListTx, "paxprotocol.paxchain.eth.AccessListTx">;
export declare const DynamicFeeTx: MessageFns<DynamicFeeTx, "paxprotocol.paxchain.eth.DynamicFeeTx">;
export declare const BlobTx: MessageFns<BlobTx, "paxprotocol.paxchain.eth.BlobTx">;
export declare const BlobTxSidecar: MessageFns<BlobTxSidecar, "paxprotocol.paxchain.eth.BlobTxSidecar">;
export declare const ExtensionOptionsEthereumTx: MessageFns<ExtensionOptionsEthereumTx, "paxprotocol.paxchain.eth.ExtensionOptionsEthereumTx">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/paxprotocol.paxchain.eth.AccessTuple": {
        aminoType: string;
        toAmino: (message: AccessTuple) => {
            address: string;
            storage_keys: string[];
        };
        fromAmino: (object: AccessTuple) => {
            address: string;
            storage_keys: string[];
        };
    };
    "/paxprotocol.paxchain.eth.AssociateTx": {
        aminoType: string;
        toAmino: (message: AssociateTx) => {
            v: Uint8Array;
            r: Uint8Array;
            s: Uint8Array;
            custom_message: string;
        };
        fromAmino: (object: AssociateTx) => {
            v: Uint8Array;
            r: Uint8Array;
            s: Uint8Array;
            custom_message: string;
        };
    };
    "/paxprotocol.paxchain.eth.LegacyTx": {
        aminoType: string;
        toAmino: (message: LegacyTx) => {
            nonce: number;
            gas_price: string;
            gas_limit: number;
            to: string;
            value: string;
            data: Uint8Array;
            v: Uint8Array;
            r: Uint8Array;
            s: Uint8Array;
        };
        fromAmino: (object: LegacyTx) => {
            nonce: number;
            gas_price: string;
            gas_limit: number;
            to: string;
            value: string;
            data: Uint8Array;
            v: Uint8Array;
            r: Uint8Array;
            s: Uint8Array;
        };
    };
    "/paxprotocol.paxchain.eth.AccessListTx": {
        aminoType: string;
        toAmino: (message: AccessListTx) => {
            chain_id: string;
            nonce: number;
            gas_price: string;
            gas_limit: number;
            to: string;
            value: string;
            data: Uint8Array;
            accesses: AccessTuple_type[];
            v: Uint8Array;
            r: Uint8Array;
            s: Uint8Array;
        };
        fromAmino: (object: AccessListTx) => {
            chain_id: string;
            nonce: number;
            gas_price: string;
            gas_limit: number;
            to: string;
            value: string;
            data: Uint8Array;
            accesses: AccessTuple_type[];
            v: Uint8Array;
            r: Uint8Array;
            s: Uint8Array;
        };
    };
    "/paxprotocol.paxchain.eth.DynamicFeeTx": {
        aminoType: string;
        toAmino: (message: DynamicFeeTx) => {
            chain_id: string;
            nonce: number;
            gas_tip_cap: string;
            gas_fee_cap: string;
            gas_limit: number;
            to: string;
            value: string;
            data: Uint8Array;
            accesses: AccessTuple_type[];
            v: Uint8Array;
            r: Uint8Array;
            s: Uint8Array;
        };
        fromAmino: (object: DynamicFeeTx) => {
            chain_id: string;
            nonce: number;
            gas_tip_cap: string;
            gas_fee_cap: string;
            gas_limit: number;
            to: string;
            value: string;
            data: Uint8Array;
            accesses: AccessTuple_type[];
            v: Uint8Array;
            r: Uint8Array;
            s: Uint8Array;
        };
    };
    "/paxprotocol.paxchain.eth.BlobTx": {
        aminoType: string;
        toAmino: (message: BlobTx) => {
            chain_id: string;
            nonce: number;
            gas_tip_cap: string;
            gas_fee_cap: string;
            gas_limit: number;
            to: string;
            value: string;
            data: Uint8Array;
            accesses: AccessTuple_type[];
            blob_fee_cap: string;
            blob_hashes: Uint8Array[];
            sidecar?: BlobTxSidecar_type;
            v: Uint8Array;
            r: Uint8Array;
            s: Uint8Array;
        };
        fromAmino: (object: BlobTx) => {
            chain_id: string;
            nonce: number;
            gas_tip_cap: string;
            gas_fee_cap: string;
            gas_limit: number;
            to: string;
            value: string;
            data: Uint8Array;
            accesses: AccessTuple_type[];
            blob_fee_cap: string;
            blob_hashes: Uint8Array[];
            sidecar?: BlobTxSidecar_type;
            v: Uint8Array;
            r: Uint8Array;
            s: Uint8Array;
        };
    };
    "/paxprotocol.paxchain.eth.BlobTxSidecar": {
        aminoType: string;
        toAmino: (message: BlobTxSidecar) => {
            blobs: Uint8Array[];
            commitments: Uint8Array[];
            proofs: Uint8Array[];
        };
        fromAmino: (object: BlobTxSidecar) => {
            blobs: Uint8Array[];
            commitments: Uint8Array[];
            proofs: Uint8Array[];
        };
    };
    "/paxprotocol.paxchain.eth.ExtensionOptionsEthereumTx": {
        aminoType: string;
        toAmino: (message: ExtensionOptionsEthereumTx) => {};
        fromAmino: (object: ExtensionOptionsEthereumTx) => {};
    };
};
