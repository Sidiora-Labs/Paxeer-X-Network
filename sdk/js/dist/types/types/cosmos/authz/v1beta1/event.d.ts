export interface EventGrant {
    /** Msg type URL for which an autorization is granted */
    msg_type_url: string;
    /** Granter account address */
    granter: string;
    /** Grantee account address */
    grantee: string;
}
export interface EventRevoke {
    /** Msg type URL for which an autorization is revoked */
    msg_type_url: string;
    /** Granter account address */
    granter: string;
    /** Grantee account address */
    grantee: string;
}
