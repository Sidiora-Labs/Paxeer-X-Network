import { AccessOperationSelectorType, AccessType, ResourceType, WasmMessageSubtype } from "../../../types/cosmos/accesscontrol";
export declare function accessTypeFromJSON(object: any): AccessType;
export declare function accessTypeToJSON(object: AccessType): string;
export declare function accessOperationSelectorTypeFromJSON(object: any): AccessOperationSelectorType;
export declare function accessOperationSelectorTypeToJSON(object: AccessOperationSelectorType): string;
export declare function resourceTypeFromJSON(object: any): ResourceType;
export declare function resourceTypeToJSON(object: ResourceType): string;
export declare function wasmMessageSubtypeFromJSON(object: any): WasmMessageSubtype;
export declare function wasmMessageSubtypeToJSON(object: WasmMessageSubtype): string;
