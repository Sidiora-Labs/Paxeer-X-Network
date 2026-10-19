import type { Any } from "../../../google/protobuf/any";
export interface GenericAuthorization {
    /** Msg, identified by it's type URL, to grant unrestricted permissions to execute */
    msg: string;
}
export interface Grant {
    authorization?: Any;
    expiration?: Date;
}
export interface GrantAuthorization {
    granter: string;
    grantee: string;
    authorization?: Any;
    expiration?: Date;
}
