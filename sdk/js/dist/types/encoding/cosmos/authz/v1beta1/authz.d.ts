import type { GeneratedType } from "@cosmjs/proto-signing";
import type { GenericAuthorization as GenericAuthorization_type, GrantAuthorization as GrantAuthorization_type, Grant as Grant_type } from "../../../../types/cosmos/authz/v1beta1";
import type { MessageFns } from "../../../common";
export interface GenericAuthorization extends GenericAuthorization_type {
}
export interface Grant extends Grant_type {
}
export interface GrantAuthorization extends GrantAuthorization_type {
}
export declare const GenericAuthorization: MessageFns<GenericAuthorization, "cosmos.authz.v1beta1.GenericAuthorization">;
export declare const Grant: MessageFns<Grant, "cosmos.authz.v1beta1.Grant">;
export declare const GrantAuthorization: MessageFns<GrantAuthorization, "cosmos.authz.v1beta1.GrantAuthorization">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.authz.v1beta1.GenericAuthorization": {
        aminoType: string;
        toAmino: (message: GenericAuthorization) => {
            msg: string;
        };
        fromAmino: (object: GenericAuthorization) => {
            msg: string;
        };
    };
    "/cosmos.authz.v1beta1.Grant": {
        aminoType: string;
        toAmino: (message: Grant) => {
            authorization?: import("../../../../types/google/protobuf").Any;
            expiration?: Date;
        };
        fromAmino: (object: Grant) => {
            authorization?: import("../../../../types/google/protobuf").Any;
            expiration?: Date;
        };
    };
    "/cosmos.authz.v1beta1.GrantAuthorization": {
        aminoType: string;
        toAmino: (message: GrantAuthorization) => {
            granter: string;
            grantee: string;
            authorization?: import("../../../../types/google/protobuf").Any;
            expiration?: Date;
        };
        fromAmino: (object: GrantAuthorization) => {
            granter: string;
            grantee: string;
            authorization?: import("../../../../types/google/protobuf").Any;
            expiration?: Date;
        };
    };
};
