import type { GeneratedType } from "@cosmjs/proto-signing";
import type { MsgGrantAllowanceResponse as MsgGrantAllowanceResponse_type, MsgGrantAllowance as MsgGrantAllowance_type, MsgRevokeAllowanceResponse as MsgRevokeAllowanceResponse_type, MsgRevokeAllowance as MsgRevokeAllowance_type } from "../../../../types/cosmos/feegrant/v1beta1";
import type { MessageFns } from "../../../common";
export interface MsgGrantAllowance extends MsgGrantAllowance_type {
}
export interface MsgGrantAllowanceResponse extends MsgGrantAllowanceResponse_type {
}
export interface MsgRevokeAllowance extends MsgRevokeAllowance_type {
}
export interface MsgRevokeAllowanceResponse extends MsgRevokeAllowanceResponse_type {
}
export declare const MsgGrantAllowance: MessageFns<MsgGrantAllowance, "cosmos.feegrant.v1beta1.MsgGrantAllowance">;
export declare const MsgGrantAllowanceResponse: MessageFns<MsgGrantAllowanceResponse, "cosmos.feegrant.v1beta1.MsgGrantAllowanceResponse">;
export declare const MsgRevokeAllowance: MessageFns<MsgRevokeAllowance, "cosmos.feegrant.v1beta1.MsgRevokeAllowance">;
export declare const MsgRevokeAllowanceResponse: MessageFns<MsgRevokeAllowanceResponse, "cosmos.feegrant.v1beta1.MsgRevokeAllowanceResponse">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.feegrant.v1beta1.MsgGrantAllowance": {
        aminoType: string;
        toAmino: (message: MsgGrantAllowance) => {
            granter: string;
            grantee: string;
            allowance?: import("../../../../types/google/protobuf").Any;
        };
        fromAmino: (object: MsgGrantAllowance) => {
            granter: string;
            grantee: string;
            allowance?: import("../../../../types/google/protobuf").Any;
        };
    };
    "/cosmos.feegrant.v1beta1.MsgGrantAllowanceResponse": {
        aminoType: string;
        toAmino: (message: MsgGrantAllowanceResponse) => {};
        fromAmino: (object: MsgGrantAllowanceResponse) => {};
    };
    "/cosmos.feegrant.v1beta1.MsgRevokeAllowance": {
        aminoType: string;
        toAmino: (message: MsgRevokeAllowance) => {
            granter: string;
            grantee: string;
        };
        fromAmino: (object: MsgRevokeAllowance) => {
            granter: string;
            grantee: string;
        };
    };
    "/cosmos.feegrant.v1beta1.MsgRevokeAllowanceResponse": {
        aminoType: string;
        toAmino: (message: MsgRevokeAllowanceResponse) => {};
        fromAmino: (object: MsgRevokeAllowanceResponse) => {};
    };
};
