import type { Coin } from "../../base/v1beta1/coin";
export declare enum AuthorizationType {
    /** AUTHORIZATION_TYPE_UNSPECIFIED - AUTHORIZATION_TYPE_UNSPECIFIED specifies an unknown authorization type */
    AUTHORIZATION_TYPE_UNSPECIFIED = 0,
    /** AUTHORIZATION_TYPE_DELEGATE - AUTHORIZATION_TYPE_DELEGATE defines an authorization type for Msg/Delegate */
    AUTHORIZATION_TYPE_DELEGATE = 1,
    /** AUTHORIZATION_TYPE_UNDELEGATE - AUTHORIZATION_TYPE_UNDELEGATE defines an authorization type for Msg/Undelegate */
    AUTHORIZATION_TYPE_UNDELEGATE = 2,
    /** AUTHORIZATION_TYPE_REDELEGATE - AUTHORIZATION_TYPE_REDELEGATE defines an authorization type for Msg/BeginRedelegate */
    AUTHORIZATION_TYPE_REDELEGATE = 3,
    UNRECOGNIZED = -1
}
export interface StakeAuthorization {
    /**
     * max_tokens specifies the maximum amount of tokens can be delegate to a validator. If it is
     * empty, there is no spend limit and any amount of coins can be delegated.
     */
    max_tokens?: Coin;
    /**
     * allow_list specifies list of validator addresses to whom grantee can delegate tokens on behalf of granter's
     * account.
     */
    allow_list?: StakeAuthorizationValidators;
    /** deny_list specifies list of validator addresses to whom grantee can not delegate tokens. */
    deny_list?: StakeAuthorizationValidators;
    /** authorization_type defines one of AuthorizationType. */
    authorization_type: AuthorizationType;
}
export interface StakeAuthorizationValidators {
    address: string[];
}
