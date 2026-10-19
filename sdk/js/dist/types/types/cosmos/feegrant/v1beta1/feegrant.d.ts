import type { Any } from "../../../google/protobuf/any";
import type { Duration } from "../../../google/protobuf/duration";
import type { Coin } from "../../base/v1beta1/coin";
export interface BasicAllowance {
    /**
     * spend_limit specifies the maximum amount of tokens that can be spent
     * by this allowance and will be updated as tokens are spent. If it is
     * empty, there is no spend limit and any amount of coins can be spent.
     */
    spend_limit: Coin[];
    /** expiration specifies an optional time when this allowance expires */
    expiration?: Date;
}
export interface PeriodicAllowance {
    /** basic specifies a struct of `BasicAllowance` */
    basic?: BasicAllowance;
    /**
     * period specifies the time duration in which period_spend_limit coins can
     * be spent before that allowance is reset
     */
    period?: Duration;
    /**
     * period_spend_limit specifies the maximum number of coins that can be spent
     * in the period
     */
    period_spend_limit: Coin[];
    /** period_can_spend is the number of coins left to be spent before the period_reset time */
    period_can_spend: Coin[];
    /**
     * period_reset is the time at which this period resets and a new one begins,
     * it is calculated from the start time of the first transaction after the
     * last period ended
     */
    period_reset?: Date;
}
export interface AllowedMsgAllowance {
    /** allowance can be any of basic and filtered fee allowance. */
    allowance?: Any;
    /** allowed_messages are the messages for which the grantee has the access. */
    allowed_messages: string[];
}
export interface Grant {
    /** granter is the address of the user granting an allowance of their funds. */
    granter: string;
    /** grantee is the address of the user being granted an allowance of another user's funds. */
    grantee: string;
    /** allowance can be any of basic and filtered fee allowance. */
    allowance?: Any;
}
