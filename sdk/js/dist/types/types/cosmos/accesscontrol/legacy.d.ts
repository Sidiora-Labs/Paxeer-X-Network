import type { AccessOperation } from "./accesscontrol";
import type { AccessOperationSelectorType } from "./constants";
export interface LegacyAccessOperationWithSelector {
    operation?: AccessOperation;
    selector_type: AccessOperationSelectorType;
    selector: string;
}
export interface LegacyWasmDependencyMapping {
    enabled: boolean;
    access_ops: LegacyAccessOperationWithSelector[];
    reset_reason: string;
    contract_address: string;
}
