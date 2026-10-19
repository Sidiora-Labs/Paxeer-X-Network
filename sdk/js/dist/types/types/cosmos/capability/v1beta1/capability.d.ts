export interface Capability {
    index: number;
}
export interface Owner {
    module: string;
    name: string;
}
export interface CapabilityOwners {
    owners: Owner[];
}
