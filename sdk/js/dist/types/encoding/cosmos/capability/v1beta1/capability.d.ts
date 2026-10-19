import type { GeneratedType } from "@cosmjs/proto-signing";
import type { CapabilityOwners as CapabilityOwners_type, Capability as Capability_type, Owner as Owner_type } from "../../../../types/cosmos/capability/v1beta1";
import type { MessageFns } from "../../../common";
export interface Capability extends Capability_type {
}
export interface Owner extends Owner_type {
}
export interface CapabilityOwners extends CapabilityOwners_type {
}
export declare const Capability: MessageFns<Capability, "cosmos.capability.v1beta1.Capability">;
export declare const Owner: MessageFns<Owner, "cosmos.capability.v1beta1.Owner">;
export declare const CapabilityOwners: MessageFns<CapabilityOwners, "cosmos.capability.v1beta1.CapabilityOwners">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.capability.v1beta1.Capability": {
        aminoType: string;
        toAmino: (message: Capability) => {
            index: number;
        };
        fromAmino: (object: Capability) => {
            index: number;
        };
    };
    "/cosmos.capability.v1beta1.Owner": {
        aminoType: string;
        toAmino: (message: Owner) => {
            module: string;
            name: string;
        };
        fromAmino: (object: Owner) => {
            module: string;
            name: string;
        };
    };
    "/cosmos.capability.v1beta1.CapabilityOwners": {
        aminoType: string;
        toAmino: (message: CapabilityOwners) => {
            owners: Owner_type[];
        };
        fromAmino: (object: CapabilityOwners) => {
            owners: Owner_type[];
        };
    };
};
