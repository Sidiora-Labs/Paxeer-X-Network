import type { GeneratedType } from "@cosmjs/proto-signing";
import type { AuthInfo as AuthInfo_type, Fee as Fee_type, ModeInfoMulti as ModeInfoMulti_type, ModeInfoSingle as ModeInfoSingle_type, ModeInfo as ModeInfo_type, SignDoc as SignDoc_type, SignerInfo as SignerInfo_type, TxBody as TxBody_type, TxRaw as TxRaw_type, Tx as Tx_type } from "../../../../types/cosmos/tx/v1beta1";
import type { MessageFns } from "../../../common";
export interface Tx extends Tx_type {
}
export interface TxRaw extends TxRaw_type {
}
export interface SignDoc extends SignDoc_type {
}
export interface TxBody extends TxBody_type {
}
export interface AuthInfo extends AuthInfo_type {
}
export interface SignerInfo extends SignerInfo_type {
}
export interface ModeInfo extends ModeInfo_type {
}
export interface ModeInfoSingle extends ModeInfoSingle_type {
}
export interface ModeInfoMulti extends ModeInfoMulti_type {
}
export interface Fee extends Fee_type {
}
export declare const Tx: MessageFns<Tx, "cosmos.tx.v1beta1.Tx">;
export declare const TxRaw: MessageFns<TxRaw, "cosmos.tx.v1beta1.TxRaw">;
export declare const SignDoc: MessageFns<SignDoc, "cosmos.tx.v1beta1.SignDoc">;
export declare const TxBody: MessageFns<TxBody, "cosmos.tx.v1beta1.TxBody">;
export declare const AuthInfo: MessageFns<AuthInfo, "cosmos.tx.v1beta1.AuthInfo">;
export declare const SignerInfo: MessageFns<SignerInfo, "cosmos.tx.v1beta1.SignerInfo">;
export declare const ModeInfo: MessageFns<ModeInfo, "cosmos.tx.v1beta1.ModeInfo">;
export declare const ModeInfoSingle: MessageFns<ModeInfoSingle, "cosmos.tx.v1beta1.ModeInfo.Single">;
export declare const ModeInfoMulti: MessageFns<ModeInfoMulti, "cosmos.tx.v1beta1.ModeInfo.Multi">;
export declare const Fee: MessageFns<Fee, "cosmos.tx.v1beta1.Fee">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.tx.v1beta1.Tx": {
        aminoType: string;
        toAmino: (message: Tx) => {
            body?: TxBody_type;
            auth_info?: AuthInfo_type;
            signatures: Uint8Array[];
        };
        fromAmino: (object: Tx) => {
            body?: TxBody_type;
            auth_info?: AuthInfo_type;
            signatures: Uint8Array[];
        };
    };
    "/cosmos.tx.v1beta1.TxRaw": {
        aminoType: string;
        toAmino: (message: TxRaw) => {
            body_bytes: Uint8Array;
            auth_info_bytes: Uint8Array;
            signatures: Uint8Array[];
        };
        fromAmino: (object: TxRaw) => {
            body_bytes: Uint8Array;
            auth_info_bytes: Uint8Array;
            signatures: Uint8Array[];
        };
    };
    "/cosmos.tx.v1beta1.SignDoc": {
        aminoType: string;
        toAmino: (message: SignDoc) => {
            body_bytes: Uint8Array;
            auth_info_bytes: Uint8Array;
            chain_id: string;
            account_number: number;
        };
        fromAmino: (object: SignDoc) => {
            body_bytes: Uint8Array;
            auth_info_bytes: Uint8Array;
            chain_id: string;
            account_number: number;
        };
    };
    "/cosmos.tx.v1beta1.TxBody": {
        aminoType: string;
        toAmino: (message: TxBody) => {
            messages: import("../../../../types/google/protobuf").Any[];
            memo: string;
            timeout_height: number;
            extension_options: import("../../../../types/google/protobuf").Any[];
            non_critical_extension_options: import("../../../../types/google/protobuf").Any[];
        };
        fromAmino: (object: TxBody) => {
            messages: import("../../../../types/google/protobuf").Any[];
            memo: string;
            timeout_height: number;
            extension_options: import("../../../../types/google/protobuf").Any[];
            non_critical_extension_options: import("../../../../types/google/protobuf").Any[];
        };
    };
    "/cosmos.tx.v1beta1.AuthInfo": {
        aminoType: string;
        toAmino: (message: AuthInfo) => {
            signer_infos: SignerInfo_type[];
            fee?: Fee_type;
        };
        fromAmino: (object: AuthInfo) => {
            signer_infos: SignerInfo_type[];
            fee?: Fee_type;
        };
    };
    "/cosmos.tx.v1beta1.SignerInfo": {
        aminoType: string;
        toAmino: (message: SignerInfo) => {
            public_key?: import("../../../../types/google/protobuf").Any;
            mode_info?: ModeInfo_type;
            sequence: number;
        };
        fromAmino: (object: SignerInfo) => {
            public_key?: import("../../../../types/google/protobuf").Any;
            mode_info?: ModeInfo_type;
            sequence: number;
        };
    };
    "/cosmos.tx.v1beta1.ModeInfo": {
        aminoType: string;
        toAmino: (message: ModeInfo) => {
            single?: ModeInfoSingle_type;
            multi?: ModeInfoMulti_type;
        };
        fromAmino: (object: ModeInfo) => {
            single?: ModeInfoSingle_type;
            multi?: ModeInfoMulti_type;
        };
    };
    "/cosmos.tx.v1beta1.ModeInfo.Single": {
        aminoType: string;
        toAmino: (message: ModeInfoSingle) => {
            mode: import("../../../../types/cosmos/tx/signing/v1beta1").SignMode;
        };
        fromAmino: (object: ModeInfoSingle) => {
            mode: import("../../../../types/cosmos/tx/signing/v1beta1").SignMode;
        };
    };
    "/cosmos.tx.v1beta1.ModeInfo.Multi": {
        aminoType: string;
        toAmino: (message: ModeInfoMulti) => {
            bitarray?: import("../../../../types/cosmos/crypto/multisig/v1beta1").CompactBitArray;
            mode_infos: ModeInfo_type[];
        };
        fromAmino: (object: ModeInfoMulti) => {
            bitarray?: import("../../../../types/cosmos/crypto/multisig/v1beta1").CompactBitArray;
            mode_infos: ModeInfo_type[];
        };
    };
    "/cosmos.tx.v1beta1.Fee": {
        aminoType: string;
        toAmino: (message: Fee) => {
            amount: import("../../../../types/cosmos/base/v1beta1").Coin[];
            gas_limit: number;
            payer: string;
            granter: string;
            gas_estimate: number;
        };
        fromAmino: (object: Fee) => {
            amount: import("../../../../types/cosmos/base/v1beta1").Coin[];
            gas_limit: number;
            payer: string;
            granter: string;
            gas_estimate: number;
        };
    };
};
