import type { GeneratedType } from "@cosmjs/proto-signing";
import type { EventGrant as EventGrant_type, EventRevoke as EventRevoke_type } from "../../../../types/cosmos/authz/v1beta1";
import type { MessageFns } from "../../../common";
export interface EventGrant extends EventGrant_type {
}
export interface EventRevoke extends EventRevoke_type {
}
export declare const EventGrant: MessageFns<EventGrant, "cosmos.authz.v1beta1.EventGrant">;
export declare const EventRevoke: MessageFns<EventRevoke, "cosmos.authz.v1beta1.EventRevoke">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.authz.v1beta1.EventGrant": {
        aminoType: string;
        toAmino: (message: EventGrant) => {
            msg_type_url: string;
            granter: string;
            grantee: string;
        };
        fromAmino: (object: EventGrant) => {
            msg_type_url: string;
            granter: string;
            grantee: string;
        };
    };
    "/cosmos.authz.v1beta1.EventRevoke": {
        aminoType: string;
        toAmino: (message: EventRevoke) => {
            msg_type_url: string;
            granter: string;
            grantee: string;
        };
        fromAmino: (object: EventRevoke) => {
            msg_type_url: string;
            granter: string;
            grantee: string;
        };
    };
};
