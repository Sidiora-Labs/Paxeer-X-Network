import type { WasmDependencyMapping } from "../accesscontrol/accesscontrol";
export interface RegisterWasmDependencyJSONFile {
    wasm_dependency_mapping?: WasmDependencyMapping;
}
export interface MsgRegisterWasmDependency {
    from_address: string;
    wasm_dependency_mapping?: WasmDependencyMapping;
}
export type MsgRegisterWasmDependencyResponse = {};
