import { useMemo } from "react";
import { API } from "@paxeer-network/types";
import { Decimal } from "@paxeer-network/utils";

export const useMaxDepositAmount = (
  sourceToken?: API.TokenInfo,
  balance?: string,
) => {
  const maxDepositAmount = useMemo(() => {
    const balanceDecimal = new Decimal(balance || 0).todp(
      sourceToken?.precision ?? 2,
      Decimal.ROUND_DOWN,
    );

    // If user_max_qty is -1 or undefined, ignore it and use balance only
    // user_max_qty = 0 means no deposit allowed
    if (
      sourceToken?.user_max_qty === -1 ||
      sourceToken?.user_max_qty === undefined
    ) {
      return balanceDecimal.toString();
    }

    // user_max_qty = 0 means no deposit allowed
    if (sourceToken?.user_max_qty === 0) {
      return "0";
    }

    const userMaxQty = new Decimal(sourceToken.user_max_qty).todp(
      sourceToken?.precision ?? 2,
      Decimal.ROUND_DOWN,
    );

    return balanceDecimal.lt(userMaxQty)
      ? balanceDecimal.toString()
      : userMaxQty.toString();
  }, [sourceToken, balance]);

  return maxDepositAmount;
};
