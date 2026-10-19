import type { GeneratedType } from "@cosmjs/proto-signing";
import type { MsgExecResponse as MsgExecResponse_type, MsgExec as MsgExec_type, MsgGrantResponse as MsgGrantResponse_type, MsgGrant as MsgGrant_type, MsgRevokeResponse as MsgRevokeResponse_type, MsgRevoke as MsgRevoke_type } from "../../../../types/cosmos/authz/v1beta1";
import type { MessageFns } from "../../../common";
export interface MsgGrant extends MsgGrant_type {
}
export interface MsgExecResponse extends MsgExecResponse_type {
}
export interface MsgExec extends MsgExec_type {
}
export interface MsgGrantResponse extends MsgGrantResponse_type {
}
export interface MsgRevoke extends MsgRevoke_type {
}
export interface MsgRevokeResponse extends MsgRevokeResponse_type {
}
export declare const MsgGrant: MessageFns<MsgGrant, "cosmos.authz.v1beta1.MsgGrant">;
export declare const MsgExecResponse: MessageFns<MsgExecResponse, "cosmos.authz.v1beta1.MsgExecResponse">;
export declare const MsgExec: MessageFns<MsgExec, "cosmos.authz.v1beta1.MsgExec">;
export declare const MsgGrantResponse: MessageFns<MsgGrantResponse, "cosmos.authz.v1beta1.MsgGrantResponse">;
export declare const MsgRevoke: MessageFns<MsgRevoke, "cosmos.authz.v1beta1.MsgRevoke">;
export declare const MsgRevokeResponse: MessageFns<MsgRevokeResponse, "cosmos.authz.v1beta1.MsgRevokeResponse">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.authz.v1beta1.MsgGrant": {
        aminoType: string;
        toAmino: (message: MsgGrant) => {
            granter: string;
            grantee: string;
            grant?: import("../../../../types/cosmos/authz/v1beta1").Grant;
        };
        fromAmino: (object: MsgGrant) => {
            granter: string;
            grantee: string;
            grant?: import("../../../../types/cosmos/authz/v1beta1").Grant;
        };
    };
    "/cosmos.authz.v1beta1.MsgExecResponse": {
        aminoType: string;
        toAmino: (message: MsgExecResponse) => {
            results: Uint8Array[];
        };
        fromAmino: (object: MsgExecResponse) => {
            results: Uint8Array[];
        };
    };
    "/cosmos.authz.v1beta1.MsgExec": {
        aminoType: string;
        toAmino: (message: MsgExec) => {
            grantee: string;
            msgs: import("../../../../types/google/protobuf").Any[];
        };
        fromAmino: (object: MsgExec) => {
            grantee: string;
            msgs: import("../../../../types/google/protobuf").Any[];
        };
    };
    "/cosmos.authz.v1beta1.MsgGrantResponse": {
        aminoType: string;
        toAmino: (message: MsgGrantResponse) => {};
        fromAmino: (object: MsgGrantResponse) => {};
    };
    "/cosmos.authz.v1beta1.MsgRevoke": {
        aminoType: string;
        toAmino: (message: MsgRevoke) => {
            granter: string;
            grantee: string;
            msg_type_url: string;
        };
        fromAmino: (object: MsgRevoke) => {
            granter: string;
            grantee: string;
            msg_type_url: string;
        };
    };
    "/cosmos.authz.v1beta1.MsgRevokeResponse": {
        aminoType: string;
        toAmino: (message: MsgRevokeResponse) => {};
        fromAmino: (object: MsgRevokeResponse) => {};
    };
};
