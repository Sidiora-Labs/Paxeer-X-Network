import type { Any } from "../../../google/protobuf/any";
export interface Plan {
    /**
     * Sets the name for the upgrade. This name will be used by the upgraded
     * version of the software to apply any special "on-upgrade" commands during
     * the first BeginBlock method after the upgrade is applied. It is also used
     * to detect whether a software version can handle a given upgrade. If no
     * upgrade handler with this name has been set in the software, it will be
     * assumed that the software is out-of-date when the upgrade Time or Height is
     * reached and the software will exit.
     */
    name: string;
    /**
     * Deprecated: Time based upgrades have been deprecated. Time based upgrade logic
     * has been removed from the SDK.
     * If this field is not empty, an error will be thrown.
     *
     * @deprecated
     */
    time?: Date;
    /**
     * The height at which the upgrade must be performed.
     * Only used if Time is not set.
     */
    height: number;
    /**
     * Any application specific upgrade info to be included on-chain
     * such as a git commit that validators could automatically upgrade to
     */
    info: string;
    /**
     * Deprecated: UpgradedClientState field has been deprecated. IBC upgrade logic has been
     * moved to the IBC module in the sub module 02-client.
     * If this field is not empty, an error will be thrown.
     *
     * @deprecated
     */
    upgraded_client_state?: Any;
}
export interface SoftwareUpgradeProposal {
    title: string;
    description: string;
    plan?: Plan;
}
export interface CancelSoftwareUpgradeProposal {
    title: string;
    description: string;
}
export interface ModuleVersion {
    /** name of the app module */
    name: string;
    /** consensus version of the app module */
    version: number;
}
