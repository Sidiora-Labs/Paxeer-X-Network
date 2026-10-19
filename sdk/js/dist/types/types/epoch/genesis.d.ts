import type { Epoch } from "./epoch";
import type { Params } from "./params";
export interface GenesisState {
    params?: Params;
    /** this line is used by starport scaffolding # genesis/proto/state */
    epoch?: Epoch;
}
