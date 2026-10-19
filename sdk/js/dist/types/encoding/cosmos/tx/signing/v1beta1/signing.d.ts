import type { GeneratedType } from "@cosmjs/proto-signing";
import type { SignatureDescriptorDataMulti as SignatureDescriptorDataMulti_type, SignatureDescriptorDataSingle as SignatureDescriptorDataSingle_type, SignatureDescriptorData as SignatureDescriptorData_type, SignatureDescriptor as SignatureDescriptor_type, SignatureDescriptors as SignatureDescriptors_type } from "../../../../../types/cosmos/tx/signing/v1beta1";
import { SignMode } from "../../../../../types/cosmos/tx/signing/v1beta1";
import type { MessageFns } from "../../../../common";
export interface SignatureDescriptors extends SignatureDescriptors_type {
}
export interface SignatureDescriptor extends SignatureDescriptor_type {
}
export interface SignatureDescriptorData extends SignatureDescriptorData_type {
}
export interface SignatureDescriptorDataSingle extends SignatureDescriptorDataSingle_type {
}
export interface SignatureDescriptorDataMulti extends SignatureDescriptorDataMulti_type {
}
export declare const SignatureDescriptors: MessageFns<SignatureDescriptors, "cosmos.tx.signing.v1beta1.SignatureDescriptors">;
export declare const SignatureDescriptor: MessageFns<SignatureDescriptor, "cosmos.tx.signing.v1beta1.SignatureDescriptor">;
export declare const SignatureDescriptorData: MessageFns<SignatureDescriptorData, "cosmos.tx.signing.v1beta1.SignatureDescriptor.Data">;
export declare const SignatureDescriptorDataSingle: MessageFns<SignatureDescriptorDataSingle, "cosmos.tx.signing.v1beta1.SignatureDescriptor.Data.Single">;
export declare const SignatureDescriptorDataMulti: MessageFns<SignatureDescriptorDataMulti, "cosmos.tx.signing.v1beta1.SignatureDescriptor.Data.Multi">;
export declare function signModeFromJSON(object: any): SignMode;
export declare function signModeToJSON(object: SignMode): string;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.tx.signing.v1beta1.SignatureDescriptors": {
        aminoType: string;
        toAmino: (message: SignatureDescriptors) => {
            signatures: SignatureDescriptor_type[];
        };
        fromAmino: (object: SignatureDescriptors) => {
            signatures: SignatureDescriptor_type[];
        };
    };
    "/cosmos.tx.signing.v1beta1.SignatureDescriptor": {
        aminoType: string;
        toAmino: (message: SignatureDescriptor) => {
            public_key?: import("../../../../../types/google/protobuf").Any;
            data?: SignatureDescriptorData_type;
            sequence: number;
        };
        fromAmino: (object: SignatureDescriptor) => {
            public_key?: import("../../../../../types/google/protobuf").Any;
            data?: SignatureDescriptorData_type;
            sequence: number;
        };
    };
    "/cosmos.tx.signing.v1beta1.SignatureDescriptor.Data": {
        aminoType: string;
        toAmino: (message: SignatureDescriptorData) => {
            single?: SignatureDescriptorDataSingle_type;
            multi?: SignatureDescriptorDataMulti_type;
        };
        fromAmino: (object: SignatureDescriptorData) => {
            single?: SignatureDescriptorDataSingle_type;
            multi?: SignatureDescriptorDataMulti_type;
        };
    };
    "/cosmos.tx.signing.v1beta1.SignatureDescriptor.Data.Single": {
        aminoType: string;
        toAmino: (message: SignatureDescriptorDataSingle) => {
            mode: SignMode;
            signature: Uint8Array;
        };
        fromAmino: (object: SignatureDescriptorDataSingle) => {
            mode: SignMode;
            signature: Uint8Array;
        };
    };
    "/cosmos.tx.signing.v1beta1.SignatureDescriptor.Data.Multi": {
        aminoType: string;
        toAmino: (message: SignatureDescriptorDataMulti) => {
            bitarray?: import("../../../../../types/cosmos/crypto/multisig/v1beta1").CompactBitArray;
            signatures: SignatureDescriptorData_type[];
        };
        fromAmino: (object: SignatureDescriptorDataMulti) => {
            bitarray?: import("../../../../../types/cosmos/crypto/multisig/v1beta1").CompactBitArray;
            signatures: SignatureDescriptorData_type[];
        };
    };
};
