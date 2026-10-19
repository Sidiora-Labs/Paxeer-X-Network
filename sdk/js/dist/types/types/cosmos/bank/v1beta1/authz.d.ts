import type { Coin } from "../../base/v1beta1/coin";
export interface SendAuthorization {
    spend_limit: Coin[];
}
