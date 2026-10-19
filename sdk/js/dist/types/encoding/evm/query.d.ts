import type { GeneratedType } from "@cosmjs/proto-signing";
import type { QueryEVMAddressByPaxAddressRequest as QueryEVMAddressByPaxAddressRequest_type, QueryEVMAddressByPaxAddressResponse as QueryEVMAddressByPaxAddressResponse_type, QueryPointeeRequest as QueryPointeeRequest_type, QueryPointeeResponse as QueryPointeeResponse_type, QueryPointerRequest as QueryPointerRequest_type, QueryPointerResponse as QueryPointerResponse_type, QueryPointerVersionRequest as QueryPointerVersionRequest_type, QueryPointerVersionResponse as QueryPointerVersionResponse_type, QueryPaxAddressByEVMAddressRequest as QueryPaxAddressByEVMAddressRequest_type, QueryPaxAddressByEVMAddressResponse as QueryPaxAddressByEVMAddressResponse_type, QueryStaticCallRequest as QueryStaticCallRequest_type, QueryStaticCallResponse as QueryStaticCallResponse_type } from "../../types/evm";
import type { MessageFns } from "../common";
export interface QueryPaxAddressByEVMAddressRequest extends QueryPaxAddressByEVMAddressRequest_type {
}
export interface QueryPaxAddressByEVMAddressResponse extends QueryPaxAddressByEVMAddressResponse_type {
}
export interface QueryEVMAddressByPaxAddressRequest extends QueryEVMAddressByPaxAddressRequest_type {
}
export interface QueryEVMAddressByPaxAddressResponse extends QueryEVMAddressByPaxAddressResponse_type {
}
export interface QueryStaticCallRequest extends QueryStaticCallRequest_type {
}
export interface QueryStaticCallResponse extends QueryStaticCallResponse_type {
}
export interface QueryPointerRequest extends QueryPointerRequest_type {
}
export interface QueryPointerResponse extends QueryPointerResponse_type {
}
export interface QueryPointerVersionRequest extends QueryPointerVersionRequest_type {
}
export interface QueryPointerVersionResponse extends QueryPointerVersionResponse_type {
}
export interface QueryPointeeRequest extends QueryPointeeRequest_type {
}
export interface QueryPointeeResponse extends QueryPointeeResponse_type {
}
export declare const QueryPaxAddressByEVMAddressRequest: MessageFns<QueryPaxAddressByEVMAddressRequest, "paxprotocol.paxchain.evm.QueryPaxAddressByEVMAddressRequest">;
export declare const QueryPaxAddressByEVMAddressResponse: MessageFns<QueryPaxAddressByEVMAddressResponse, "paxprotocol.paxchain.evm.QueryPaxAddressByEVMAddressResponse">;
export declare const QueryEVMAddressByPaxAddressRequest: MessageFns<QueryEVMAddressByPaxAddressRequest, "paxprotocol.paxchain.evm.QueryEVMAddressByPaxAddressRequest">;
export declare const QueryEVMAddressByPaxAddressResponse: MessageFns<QueryEVMAddressByPaxAddressResponse, "paxprotocol.paxchain.evm.QueryEVMAddressByPaxAddressResponse">;
export declare const QueryStaticCallRequest: MessageFns<QueryStaticCallRequest, "paxprotocol.paxchain.evm.QueryStaticCallRequest">;
export declare const QueryStaticCallResponse: MessageFns<QueryStaticCallResponse, "paxprotocol.paxchain.evm.QueryStaticCallResponse">;
export declare const QueryPointerRequest: MessageFns<QueryPointerRequest, "paxprotocol.paxchain.evm.QueryPointerRequest">;
export declare const QueryPointerResponse: MessageFns<QueryPointerResponse, "paxprotocol.paxchain.evm.QueryPointerResponse">;
export declare const QueryPointerVersionRequest: MessageFns<QueryPointerVersionRequest, "paxprotocol.paxchain.evm.QueryPointerVersionRequest">;
export declare const QueryPointerVersionResponse: MessageFns<QueryPointerVersionResponse, "paxprotocol.paxchain.evm.QueryPointerVersionResponse">;
export declare const QueryPointeeRequest: MessageFns<QueryPointeeRequest, "paxprotocol.paxchain.evm.QueryPointeeRequest">;
export declare const QueryPointeeResponse: MessageFns<QueryPointeeResponse, "paxprotocol.paxchain.evm.QueryPointeeResponse">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/paxprotocol.paxchain.evm.QueryPaxAddressByEVMAddressRequest": {
        aminoType: string;
        toAmino: (message: QueryPaxAddressByEVMAddressRequest) => {
            evm_address: string;
        };
        fromAmino: (object: QueryPaxAddressByEVMAddressRequest) => {
            evm_address: string;
        };
    };
    "/paxprotocol.paxchain.evm.QueryPaxAddressByEVMAddressResponse": {
        aminoType: string;
        toAmino: (message: QueryPaxAddressByEVMAddressResponse) => {
            pax_address: string;
            associated: boolean;
        };
        fromAmino: (object: QueryPaxAddressByEVMAddressResponse) => {
            pax_address: string;
            associated: boolean;
        };
    };
    "/paxprotocol.paxchain.evm.QueryEVMAddressByPaxAddressRequest": {
        aminoType: string;
        toAmino: (message: QueryEVMAddressByPaxAddressRequest) => {
            pax_address: string;
        };
        fromAmino: (object: QueryEVMAddressByPaxAddressRequest) => {
            pax_address: string;
        };
    };
    "/paxprotocol.paxchain.evm.QueryEVMAddressByPaxAddressResponse": {
        aminoType: string;
        toAmino: (message: QueryEVMAddressByPaxAddressResponse) => {
            evm_address: string;
            associated: boolean;
        };
        fromAmino: (object: QueryEVMAddressByPaxAddressResponse) => {
            evm_address: string;
            associated: boolean;
        };
    };
    "/paxprotocol.paxchain.evm.QueryStaticCallRequest": {
        aminoType: string;
        toAmino: (message: QueryStaticCallRequest) => {
            data: Uint8Array;
            to: string;
        };
        fromAmino: (object: QueryStaticCallRequest) => {
            data: Uint8Array;
            to: string;
        };
    };
    "/paxprotocol.paxchain.evm.QueryStaticCallResponse": {
        aminoType: string;
        toAmino: (message: QueryStaticCallResponse) => {
            data: Uint8Array;
        };
        fromAmino: (object: QueryStaticCallResponse) => {
            data: Uint8Array;
        };
    };
    "/paxprotocol.paxchain.evm.QueryPointerRequest": {
        aminoType: string;
        toAmino: (message: QueryPointerRequest) => {
            pointer_type: import("../../types/evm").PointerType;
            pointee: string;
        };
        fromAmino: (object: QueryPointerRequest) => {
            pointer_type: import("../../types/evm").PointerType;
            pointee: string;
        };
    };
    "/paxprotocol.paxchain.evm.QueryPointerResponse": {
        aminoType: string;
        toAmino: (message: QueryPointerResponse) => {
            pointer: string;
            version: number;
            exists: boolean;
        };
        fromAmino: (object: QueryPointerResponse) => {
            pointer: string;
            version: number;
            exists: boolean;
        };
    };
    "/paxprotocol.paxchain.evm.QueryPointerVersionRequest": {
        aminoType: string;
        toAmino: (message: QueryPointerVersionRequest) => {
            pointer_type: import("../../types/evm").PointerType;
        };
        fromAmino: (object: QueryPointerVersionRequest) => {
            pointer_type: import("../../types/evm").PointerType;
        };
    };
    "/paxprotocol.paxchain.evm.QueryPointerVersionResponse": {
        aminoType: string;
        toAmino: (message: QueryPointerVersionResponse) => {
            version: number;
            cw_code_id: number;
        };
        fromAmino: (object: QueryPointerVersionResponse) => {
            version: number;
            cw_code_id: number;
        };
    };
    "/paxprotocol.paxchain.evm.QueryPointeeRequest": {
        aminoType: string;
        toAmino: (message: QueryPointeeRequest) => {
            pointer_type: import("../../types/evm").PointerType;
            pointer: string;
        };
        fromAmino: (object: QueryPointeeRequest) => {
            pointer_type: import("../../types/evm").PointerType;
            pointer: string;
        };
    };
    "/paxprotocol.paxchain.evm.QueryPointeeResponse": {
        aminoType: string;
        toAmino: (message: QueryPointeeResponse) => {
            pointee: string;
            version: number;
            exists: boolean;
        };
        fromAmino: (object: QueryPointeeResponse) => {
            pointee: string;
            version: number;
            exists: boolean;
        };
    };
};
