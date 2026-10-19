import type { GeneratedType } from "@cosmjs/proto-signing";
import type { ListAllInterfacesRequest as ListAllInterfacesRequest_type, ListAllInterfacesResponse as ListAllInterfacesResponse_type, ListImplementationsRequest as ListImplementationsRequest_type, ListImplementationsResponse as ListImplementationsResponse_type } from "../../../../../types/cosmos/base/reflection/v1beta1";
import type { MessageFns } from "../../../../common";
export interface ListAllInterfacesRequest extends ListAllInterfacesRequest_type {
}
export interface ListAllInterfacesResponse extends ListAllInterfacesResponse_type {
}
export interface ListImplementationsRequest extends ListImplementationsRequest_type {
}
export interface ListImplementationsResponse extends ListImplementationsResponse_type {
}
export declare const ListAllInterfacesRequest: MessageFns<ListAllInterfacesRequest, "cosmos.base.reflection.v1beta1.ListAllInterfacesRequest">;
export declare const ListAllInterfacesResponse: MessageFns<ListAllInterfacesResponse, "cosmos.base.reflection.v1beta1.ListAllInterfacesResponse">;
export declare const ListImplementationsRequest: MessageFns<ListImplementationsRequest, "cosmos.base.reflection.v1beta1.ListImplementationsRequest">;
export declare const ListImplementationsResponse: MessageFns<ListImplementationsResponse, "cosmos.base.reflection.v1beta1.ListImplementationsResponse">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.base.reflection.v1beta1.ListAllInterfacesRequest": {
        aminoType: string;
        toAmino: (message: ListAllInterfacesRequest) => {};
        fromAmino: (object: ListAllInterfacesRequest) => {};
    };
    "/cosmos.base.reflection.v1beta1.ListAllInterfacesResponse": {
        aminoType: string;
        toAmino: (message: ListAllInterfacesResponse) => {
            interface_names: string[];
        };
        fromAmino: (object: ListAllInterfacesResponse) => {
            interface_names: string[];
        };
    };
    "/cosmos.base.reflection.v1beta1.ListImplementationsRequest": {
        aminoType: string;
        toAmino: (message: ListImplementationsRequest) => {
            interface_name: string;
        };
        fromAmino: (object: ListImplementationsRequest) => {
            interface_name: string;
        };
    };
    "/cosmos.base.reflection.v1beta1.ListImplementationsResponse": {
        aminoType: string;
        toAmino: (message: ListImplementationsResponse) => {
            implementation_message_names: string[];
        };
        fromAmino: (object: ListImplementationsResponse) => {
            implementation_message_names: string[];
        };
    };
};
