import type { Any } from "../../../google/protobuf/any";
import type { Grant } from "./authz";
export interface MsgGrant {
    granter: string;
    grantee: string;
    grant?: Grant;
}
export interface MsgExecResponse {
    results: Uint8Array[];
}
export interface MsgExec {
    grantee: string;
    /**
     * Authorization Msg requests to execute. Each msg must implement Authorization interface
     * The x/authz will try to find a grant matching (msg.signers[0], grantee, MsgTypeURL(msg))
     * triple and validate it.
     */
    msgs: Any[];
}
export type MsgGrantResponse = {};
export interface MsgRevoke {
    granter: string;
    grantee: string;
    msg_type_url: string;
}
export type MsgRevokeResponse = {};
