import { useMemo } from "react";
import { account as accountPerp } from "@paxeer-network/perp";
import { Decimal } from "@paxeer-network/utils";
import {
  useCollateral,
  useIndexPricesStream,
  useUpdatedRef,
  useTokenInfo,
} from "..";
import { useAppStore } from "./appStore";
import { useHoldingStream } from "./useHoldingStream";

const { maxWithdrawalUSDC, maxWithdrawalOtherCollateral, collateralRatio } =
  accountPerp;

/**
 * The max withdrawal amount for the token
 * if token is not provided, return the max withdrawal amount for USDC
 */
export const useMaxWithdrawal = (token: string) => {
  const { unsettledPnL } = useCollateral();

  const { freeCollateral } = useAppStore((state) => state.portfolio);

  const tokenInfo = useTokenInfo(token);

  const { getIndexPrice } = useIndexPricesStream();

  const indexPriceRef = useUpdatedRef(getIndexPrice(token));

  const { usdc, data: holdings = [] } = useHoldingStream();

  const holding = useMemo(() => {
    return holdings.find(
      (item) => item.token?.toUpperCase() === token.toUpperCase(),
    );
  }, [holdings, token]);

  const usdcBalance = usdc?.holding ?? 0;

  const memoizedCollateralRatio = useMemo(() => {
    const { base_weight = 0, discount_factor = 0 } = tokenInfo || {};
    const holdingQty = holding?.holding ?? 0;
    return collateralRatio({
      baseWeight: base_weight,
      discountFactor: discount_factor,
      collateralQty: holdingQty,
      collateralCap: tokenInfo?.user_max_qty ?? holdingQty,
      indexPrice: indexPriceRef.current,
    });
  }, [tokenInfo, holding?.holding, indexPriceRef]);

  const maxAmount = useMemo<number>(() => {
    if (!token) {
      return 0;
    }

    let quantity = 0;

    if (token === "USDC") {
      quantity = maxWithdrawalUSDC({
        USDCBalance: usdcBalance,
        freeCollateral,
        upnl: unsettledPnL ?? 0,
      });
    } else {
      quantity = maxWithdrawalOtherCollateral({
        USDCBalance: usdcBalance,
        collateralQty: holding?.holding ?? 0,
        freeCollateral,
        indexPrice: indexPriceRef.current,
        weight: memoizedCollateralRatio,
      }).toNumber();
    }

    if (Number.isNaN(quantity)) {
      return 0;
    }

    if (tokenInfo?.decimals === undefined) {
      return quantity;
    }

    return new Decimal(quantity || 0)
      .todp(tokenInfo.decimals, Decimal.ROUND_DOWN)
      .toNumber();
  }, [
    token,
    tokenInfo?.decimals,
    usdcBalance,
    freeCollateral,
    unsettledPnL,
    holding?.holding,
    indexPriceRef,
    memoizedCollateralRatio,
  ]);

  return maxAmount;
};
