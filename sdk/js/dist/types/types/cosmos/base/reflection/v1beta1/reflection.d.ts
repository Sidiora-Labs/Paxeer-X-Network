export type ListAllInterfacesRequest = {};
export interface ListAllInterfacesResponse {
    /** interface_names is an array of all the registered interfaces. */
    interface_names: string[];
}
export interface ListImplementationsRequest {
    /** interface_name defines the interface to query the implementations for. */
    interface_name: string;
}
export interface ListImplementationsResponse {
    implementation_message_names: string[];
}
