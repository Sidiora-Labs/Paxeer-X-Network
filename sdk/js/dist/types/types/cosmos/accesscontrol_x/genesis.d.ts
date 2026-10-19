import type { MessageDependencyMapping, WasmDependencyMapping } from "../accesscontrol/accesscontrol";
export interface GenesisState {
    params?: Params;
    /** mapping between every message type and its predetermined resource read/write sequence */
    message_dependency_mapping: MessageDependencyMapping[];
    wasm_dependency_mappings: WasmDependencyMapping[];
}
export type Params = {};
