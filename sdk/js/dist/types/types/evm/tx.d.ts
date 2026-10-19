import type { Coin } from "../cosmos/base/v1beta1/coin";
import type { Any } from "../google/protobuf/any";
import type { AssetType, PointerType } from "./enums";
import type { Log } from "./receipt";
export interface MsgEVMTransaction {
    data?: Any;
    derived: Uint8Array;
}
export interface MsgEVMTransactionResponse {
    gas_used: number;
    vm_error: string;
    return_data: Uint8Array;
    hash: string;
    logs: Log[];
}
export interface MsgInternalEVMCall {
    sender: string;
    value: string;
    to: string;
    data: Uint8Array;
}
export type MsgInternalEVMCallResponse = {};
export interface MsgInternalEVMDelegateCall {
    sender: string;
    codeHash: Uint8Array;
    to: string;
    data: Uint8Array;
    fromContract: string;
}
export type MsgInternalEVMDelegateCallResponse = {};
export interface MsgSend {
    from_address: string;
    to_address: string;
    amount: Coin[];
}
export type MsgSendResponse = {};
export interface MsgRegisterPointer {
    sender: string;
    pointer_type: PointerType;
    erc_address: string;
}
export interface MsgRegisterPointerResponse {
    pointer_address: string;
}
export interface MsgAssociateContractAddress {
    sender: string;
    address: string;
}
export type MsgAssociateContractAddressResponse = {};
export interface MsgAssociate {
    sender: string;
    custom_message: string;
}
export type MsgAssociateResponse = {};
export interface MsgClaim {
    sender: string;
    claimer: string;
}
export interface Asset {
    asset_type: AssetType;
    contract_address: string;
}
export interface MsgClaimSpecific {
    sender: string;
    claimer: string;
    assets: Asset[];
}
