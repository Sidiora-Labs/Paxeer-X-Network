import type { GeneratedType } from "@cosmjs/proto-signing";
import type { Asset as Asset_type, MsgAssociateContractAddressResponse as MsgAssociateContractAddressResponse_type, MsgAssociateContractAddress as MsgAssociateContractAddress_type, MsgAssociateResponse as MsgAssociateResponse_type, MsgAssociate as MsgAssociate_type, MsgClaimSpecific as MsgClaimSpecific_type, MsgClaim as MsgClaim_type, MsgEVMTransactionResponse as MsgEVMTransactionResponse_type, MsgEVMTransaction as MsgEVMTransaction_type, MsgInternalEVMCallResponse as MsgInternalEVMCallResponse_type, MsgInternalEVMCall as MsgInternalEVMCall_type, MsgInternalEVMDelegateCallResponse as MsgInternalEVMDelegateCallResponse_type, MsgInternalEVMDelegateCall as MsgInternalEVMDelegateCall_type, MsgRegisterPointerResponse as MsgRegisterPointerResponse_type, MsgRegisterPointer as MsgRegisterPointer_type, MsgSendResponse as MsgSendResponse_type, MsgSend as MsgSend_type } from "../../types/evm";
import type { MessageFns } from "../common";
export interface MsgEVMTransaction extends MsgEVMTransaction_type {
}
export interface MsgEVMTransactionResponse extends MsgEVMTransactionResponse_type {
}
export interface MsgInternalEVMCall extends MsgInternalEVMCall_type {
}
export interface MsgInternalEVMCallResponse extends MsgInternalEVMCallResponse_type {
}
export interface MsgInternalEVMDelegateCall extends MsgInternalEVMDelegateCall_type {
}
export interface MsgInternalEVMDelegateCallResponse extends MsgInternalEVMDelegateCallResponse_type {
}
export interface MsgSend extends MsgSend_type {
}
export interface MsgSendResponse extends MsgSendResponse_type {
}
export interface MsgRegisterPointer extends MsgRegisterPointer_type {
}
export interface MsgRegisterPointerResponse extends MsgRegisterPointerResponse_type {
}
export interface MsgAssociateContractAddress extends MsgAssociateContractAddress_type {
}
export interface MsgAssociateContractAddressResponse extends MsgAssociateContractAddressResponse_type {
}
export interface MsgAssociate extends MsgAssociate_type {
}
export interface MsgAssociateResponse extends MsgAssociateResponse_type {
}
export interface MsgClaim extends MsgClaim_type {
}
export interface Asset extends Asset_type {
}
export interface MsgClaimSpecific extends MsgClaimSpecific_type {
}
export declare const MsgEVMTransaction: MessageFns<MsgEVMTransaction, "paxprotocol.paxchain.evm.MsgEVMTransaction">;
export declare const MsgEVMTransactionResponse: MessageFns<MsgEVMTransactionResponse, "paxprotocol.paxchain.evm.MsgEVMTransactionResponse">;
export declare const MsgInternalEVMCall: MessageFns<MsgInternalEVMCall, "paxprotocol.paxchain.evm.MsgInternalEVMCall">;
export declare const MsgInternalEVMCallResponse: MessageFns<MsgInternalEVMCallResponse, "paxprotocol.paxchain.evm.MsgInternalEVMCallResponse">;
export declare const MsgInternalEVMDelegateCall: MessageFns<MsgInternalEVMDelegateCall, "paxprotocol.paxchain.evm.MsgInternalEVMDelegateCall">;
export declare const MsgInternalEVMDelegateCallResponse: MessageFns<MsgInternalEVMDelegateCallResponse, "paxprotocol.paxchain.evm.MsgInternalEVMDelegateCallResponse">;
export declare const MsgSend: MessageFns<MsgSend, "paxprotocol.paxchain.evm.MsgSend">;
export declare const MsgSendResponse: MessageFns<MsgSendResponse, "paxprotocol.paxchain.evm.MsgSendResponse">;
export declare const MsgRegisterPointer: MessageFns<MsgRegisterPointer, "paxprotocol.paxchain.evm.MsgRegisterPointer">;
export declare const MsgRegisterPointerResponse: MessageFns<MsgRegisterPointerResponse, "paxprotocol.paxchain.evm.MsgRegisterPointerResponse">;
export declare const MsgAssociateContractAddress: MessageFns<MsgAssociateContractAddress, "paxprotocol.paxchain.evm.MsgAssociateContractAddress">;
export declare const MsgAssociateContractAddressResponse: MessageFns<MsgAssociateContractAddressResponse, "paxprotocol.paxchain.evm.MsgAssociateContractAddressResponse">;
export declare const MsgAssociate: MessageFns<MsgAssociate, "paxprotocol.paxchain.evm.MsgAssociate">;
export declare const MsgAssociateResponse: MessageFns<MsgAssociateResponse, "paxprotocol.paxchain.evm.MsgAssociateResponse">;
export declare const MsgClaim: MessageFns<MsgClaim, "paxprotocol.paxchain.evm.MsgClaim">;
export declare const Asset: MessageFns<Asset, "paxprotocol.paxchain.evm.Asset">;
export declare const MsgClaimSpecific: MessageFns<MsgClaimSpecific, "paxprotocol.paxchain.evm.MsgClaimSpecific">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/paxprotocol.paxchain.evm.MsgEVMTransaction": {
        aminoType: string;
        toAmino: (message: MsgEVMTransaction) => {
            data?: import("../../types/google/protobuf").Any;
            derived: Uint8Array;
        };
        fromAmino: (object: MsgEVMTransaction) => {
            data?: import("../../types/google/protobuf").Any;
            derived: Uint8Array;
        };
    };
    "/paxprotocol.paxchain.evm.MsgEVMTransactionResponse": {
        aminoType: string;
        toAmino: (message: MsgEVMTransactionResponse) => {
            gas_used: number;
            vm_error: string;
            return_data: Uint8Array;
            hash: string;
            logs: import("../../types/evm").Log[];
        };
        fromAmino: (object: MsgEVMTransactionResponse) => {
            gas_used: number;
            vm_error: string;
            return_data: Uint8Array;
            hash: string;
            logs: import("../../types/evm").Log[];
        };
    };
    "/paxprotocol.paxchain.evm.MsgInternalEVMCall": {
        aminoType: string;
        toAmino: (message: MsgInternalEVMCall) => {
            sender: string;
            value: string;
            to: string;
            data: Uint8Array;
        };
        fromAmino: (object: MsgInternalEVMCall) => {
            sender: string;
            value: string;
            to: string;
            data: Uint8Array;
        };
    };
    "/paxprotocol.paxchain.evm.MsgInternalEVMCallResponse": {
        aminoType: string;
        toAmino: (message: MsgInternalEVMCallResponse) => {};
        fromAmino: (object: MsgInternalEVMCallResponse) => {};
    };
    "/paxprotocol.paxchain.evm.MsgInternalEVMDelegateCall": {
        aminoType: string;
        toAmino: (message: MsgInternalEVMDelegateCall) => {
            sender: string;
            codeHash: Uint8Array;
            to: string;
            data: Uint8Array;
            fromContract: string;
        };
        fromAmino: (object: MsgInternalEVMDelegateCall) => {
            sender: string;
            codeHash: Uint8Array;
            to: string;
            data: Uint8Array;
            fromContract: string;
        };
    };
    "/paxprotocol.paxchain.evm.MsgInternalEVMDelegateCallResponse": {
        aminoType: string;
        toAmino: (message: MsgInternalEVMDelegateCallResponse) => {};
        fromAmino: (object: MsgInternalEVMDelegateCallResponse) => {};
    };
    "/paxprotocol.paxchain.evm.MsgSend": {
        aminoType: string;
        toAmino: (message: MsgSend) => {
            from_address: string;
            to_address: string;
            amount: import("../../types/cosmos/base/v1beta1").Coin[];
        };
        fromAmino: (object: MsgSend) => {
            from_address: string;
            to_address: string;
            amount: import("../../types/cosmos/base/v1beta1").Coin[];
        };
    };
    "/paxprotocol.paxchain.evm.MsgSendResponse": {
        aminoType: string;
        toAmino: (message: MsgSendResponse) => {};
        fromAmino: (object: MsgSendResponse) => {};
    };
    "/paxprotocol.paxchain.evm.MsgRegisterPointer": {
        aminoType: string;
        toAmino: (message: MsgRegisterPointer) => {
            sender: string;
            pointer_type: import("../../types/evm").PointerType;
            erc_address: string;
        };
        fromAmino: (object: MsgRegisterPointer) => {
            sender: string;
            pointer_type: import("../../types/evm").PointerType;
            erc_address: string;
        };
    };
    "/paxprotocol.paxchain.evm.MsgRegisterPointerResponse": {
        aminoType: string;
        toAmino: (message: MsgRegisterPointerResponse) => {
            pointer_address: string;
        };
        fromAmino: (object: MsgRegisterPointerResponse) => {
            pointer_address: string;
        };
    };
    "/paxprotocol.paxchain.evm.MsgAssociateContractAddress": {
        aminoType: string;
        toAmino: (message: MsgAssociateContractAddress) => {
            sender: string;
            address: string;
        };
        fromAmino: (object: MsgAssociateContractAddress) => {
            sender: string;
            address: string;
        };
    };
    "/paxprotocol.paxchain.evm.MsgAssociateContractAddressResponse": {
        aminoType: string;
        toAmino: (message: MsgAssociateContractAddressResponse) => {};
        fromAmino: (object: MsgAssociateContractAddressResponse) => {};
    };
    "/paxprotocol.paxchain.evm.MsgAssociate": {
        aminoType: string;
        toAmino: (message: MsgAssociate) => {
            sender: string;
            custom_message: string;
        };
        fromAmino: (object: MsgAssociate) => {
            sender: string;
            custom_message: string;
        };
    };
    "/paxprotocol.paxchain.evm.MsgAssociateResponse": {
        aminoType: string;
        toAmino: (message: MsgAssociateResponse) => {};
        fromAmino: (object: MsgAssociateResponse) => {};
    };
    "/paxprotocol.paxchain.evm.MsgClaim": {
        aminoType: string;
        toAmino: (message: MsgClaim) => {
            sender: string;
            claimer: string;
        };
        fromAmino: (object: MsgClaim) => {
            sender: string;
            claimer: string;
        };
    };
    "/paxprotocol.paxchain.evm.Asset": {
        aminoType: string;
        toAmino: (message: Asset) => {
            asset_type: import("../../types/evm").AssetType;
            contract_address: string;
        };
        fromAmino: (object: Asset) => {
            asset_type: import("../../types/evm").AssetType;
            contract_address: string;
        };
    };
    "/paxprotocol.paxchain.evm.MsgClaimSpecific": {
        aminoType: string;
        toAmino: (message: MsgClaimSpecific) => {
            sender: string;
            claimer: string;
            assets: Asset_type[];
        };
        fromAmino: (object: MsgClaimSpecific) => {
            sender: string;
            claimer: string;
            assets: Asset_type[];
        };
    };
};
