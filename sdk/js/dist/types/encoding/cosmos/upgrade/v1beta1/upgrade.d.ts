import type { GeneratedType } from "@cosmjs/proto-signing";
import type { CancelSoftwareUpgradeProposal as CancelSoftwareUpgradeProposal_type, ModuleVersion as ModuleVersion_type, Plan as Plan_type, SoftwareUpgradeProposal as SoftwareUpgradeProposal_type } from "../../../../types/cosmos/upgrade/v1beta1";
import type { MessageFns } from "../../../common";
export interface Plan extends Plan_type {
}
export interface SoftwareUpgradeProposal extends SoftwareUpgradeProposal_type {
}
export interface CancelSoftwareUpgradeProposal extends CancelSoftwareUpgradeProposal_type {
}
export interface ModuleVersion extends ModuleVersion_type {
}
export declare const Plan: MessageFns<Plan, "cosmos.upgrade.v1beta1.Plan">;
export declare const SoftwareUpgradeProposal: MessageFns<SoftwareUpgradeProposal, "cosmos.upgrade.v1beta1.SoftwareUpgradeProposal">;
export declare const CancelSoftwareUpgradeProposal: MessageFns<CancelSoftwareUpgradeProposal, "cosmos.upgrade.v1beta1.CancelSoftwareUpgradeProposal">;
export declare const ModuleVersion: MessageFns<ModuleVersion, "cosmos.upgrade.v1beta1.ModuleVersion">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.upgrade.v1beta1.Plan": {
        aminoType: string;
        toAmino: (message: Plan) => {
            name: string;
            time?: Date;
            height: number;
            info: string;
            upgraded_client_state?: import("../../../../types/google/protobuf").Any;
        };
        fromAmino: (object: Plan) => {
            name: string;
            time?: Date;
            height: number;
            info: string;
            upgraded_client_state?: import("../../../../types/google/protobuf").Any;
        };
    };
    "/cosmos.upgrade.v1beta1.SoftwareUpgradeProposal": {
        aminoType: string;
        toAmino: (message: SoftwareUpgradeProposal) => {
            title: string;
            description: string;
            plan?: Plan_type;
        };
        fromAmino: (object: SoftwareUpgradeProposal) => {
            title: string;
            description: string;
            plan?: Plan_type;
        };
    };
    "/cosmos.upgrade.v1beta1.CancelSoftwareUpgradeProposal": {
        aminoType: string;
        toAmino: (message: CancelSoftwareUpgradeProposal) => {
            title: string;
            description: string;
        };
        fromAmino: (object: CancelSoftwareUpgradeProposal) => {
            title: string;
            description: string;
        };
    };
    "/cosmos.upgrade.v1beta1.ModuleVersion": {
        aminoType: string;
        toAmino: (message: ModuleVersion) => {
            name: string;
            version: number;
        };
        fromAmino: (object: ModuleVersion) => {
            name: string;
            version: number;
        };
    };
};
