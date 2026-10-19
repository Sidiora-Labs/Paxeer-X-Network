import type { GeneratedType } from "@cosmjs/proto-signing";
import type { GenesisOwners as GenesisOwners_type, GenesisState as GenesisState_type } from "../../../../types/cosmos/capability/v1beta1";
import type { MessageFns } from "../../../common";
export interface GenesisOwners extends GenesisOwners_type {
}
export interface GenesisState extends GenesisState_type {
}
export declare const GenesisOwners: MessageFns<GenesisOwners, "cosmos.capability.v1beta1.GenesisOwners">;
export declare const GenesisState: MessageFns<GenesisState, "cosmos.capability.v1beta1.GenesisState">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.capability.v1beta1.GenesisOwners": {
        aminoType: string;
        toAmino: (message: GenesisOwners) => {
            index: number;
            index_owners?: import("../../../../types/cosmos/capability/v1beta1").CapabilityOwners;
        };
        fromAmino: (object: GenesisOwners) => {
            index: number;
            index_owners?: import("../../../../types/cosmos/capability/v1beta1").CapabilityOwners;
        };
    };
    "/cosmos.capability.v1beta1.GenesisState": {
        aminoType: string;
        toAmino: (message: GenesisState) => {
            index: number;
            owners: GenesisOwners_type[];
        };
        fromAmino: (object: GenesisState) => {
            index: number;
            owners: GenesisOwners_type[];
        };
    };
};
