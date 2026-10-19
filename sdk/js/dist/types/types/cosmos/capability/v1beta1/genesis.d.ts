import type { CapabilityOwners } from "./capability";
export interface GenesisOwners {
    /** index is the index of the capability owner. */
    index: number;
    /** index_owners are the owners at the given index. */
    index_owners?: CapabilityOwners;
}
export interface GenesisState {
    /** index is the capability global index. */
    index: number;
    /**
     * owners represents a map from index to owners of the capability index
     * index key is string to allow amino marshalling.
     */
    owners: GenesisOwners[];
}
