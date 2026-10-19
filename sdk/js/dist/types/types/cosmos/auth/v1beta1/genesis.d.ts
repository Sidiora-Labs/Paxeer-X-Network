import type { Any } from "../../../google/protobuf/any";
import type { Params } from "./auth";
export interface GenesisState {
    /** params defines all the paramaters of the module. */
    params?: Params;
    /** accounts are the accounts present at genesis. */
    accounts: Any[];
}
