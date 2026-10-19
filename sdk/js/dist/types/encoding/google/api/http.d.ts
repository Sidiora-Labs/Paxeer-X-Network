import type { GeneratedType } from "@cosmjs/proto-signing";
import type { CustomHttpPattern as CustomHttpPattern_type, HttpRule as HttpRule_type, Http as Http_type } from "../../../types/google/api";
import type { MessageFns } from "../../common";
export interface Http extends Http_type {
}
export interface HttpRule extends HttpRule_type {
}
export interface CustomHttpPattern extends CustomHttpPattern_type {
}
export declare const Http: MessageFns<Http, "google.api.Http">;
export declare const HttpRule: MessageFns<HttpRule, "google.api.HttpRule">;
export declare const CustomHttpPattern: MessageFns<CustomHttpPattern, "google.api.CustomHttpPattern">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/google.api.Http": {
        aminoType: string;
        toAmino: (message: Http) => {
            rules: HttpRule_type[];
            fully_decode_reserved_expansion: boolean;
        };
        fromAmino: (object: Http) => {
            rules: HttpRule_type[];
            fully_decode_reserved_expansion: boolean;
        };
    };
    "/google.api.HttpRule": {
        aminoType: string;
        toAmino: (message: HttpRule) => {
            selector: string;
            get?: string;
            put?: string;
            post?: string;
            delete?: string;
            patch?: string;
            custom?: CustomHttpPattern_type;
            body: string;
            response_body: string;
            additional_bindings: HttpRule_type[];
        };
        fromAmino: (object: HttpRule) => {
            selector: string;
            get?: string;
            put?: string;
            post?: string;
            delete?: string;
            patch?: string;
            custom?: CustomHttpPattern_type;
            body: string;
            response_body: string;
            additional_bindings: HttpRule_type[];
        };
    };
    "/google.api.CustomHttpPattern": {
        aminoType: string;
        toAmino: (message: CustomHttpPattern) => {
            kind: string;
            path: string;
        };
        fromAmino: (object: CustomHttpPattern) => {
            kind: string;
            path: string;
        };
    };
};
