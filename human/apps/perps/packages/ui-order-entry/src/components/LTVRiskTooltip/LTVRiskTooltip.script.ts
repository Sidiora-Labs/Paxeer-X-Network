import { useCallback } from "react";
import {
  useAppStore,
  useComputedLTV,
  useConvertThreshold,
  useHoldingStream,
  useIndexPricesStream,
} from "@paxeer-network/hooks";
import { account } from "@paxeer-network/perp";
import { MarginMode } from "@paxeer-network/types";
import { modal, useScreen } from "@paxeer-network/ui";
import { Decimal, zero } from "@paxeer-network/utils";

export const useLTVTooltipScript = (marginMode?: MarginMode) => {
  const { isMobile } = useScreen();
  const { data: holdingList = [], isLoading: isHoldingLoading } =
    useHoldingStream();

  const {
    ltv_threshold,
    negative_usdc_threshold,
    isLoading: isThresholdLoading,
  } = useConvertThreshold();

  // The shared hook returns the raw ratio (e.g. 0.9); the tooltip renders a percentage.
  const ltvThresholdPercentage = new Decimal(ltv_threshold ?? 0)
    .mul(100)
    .toNumber();

  const tokensInfo = useAppStore((state) => state.tokensInfo);

  const { getIndexPrice } = useIndexPricesStream();

  const holdingData = holdingList.map((item) => {
    const tokenInfo = tokensInfo?.find(({ token }) => token === item.token);

    // Use extracted function for index price calculation
    const indexPrice = getIndexPrice(item.token);

    // Calculate collateral ratio for this token
    const collateralRatio = tokenInfo
      ? account.collateralRatio({
          baseWeight: tokenInfo.base_weight ?? 0,
          discountFactor: tokenInfo.discount_factor ?? 0,
          collateralQty: item.holding,
          collateralCap: tokenInfo?.user_max_qty ?? item.holding,
          indexPrice: indexPrice,
        })
      : zero;

    // Calculate collateral contribution for this token
    const collateralContribution = account.collateralContribution({
      collateralQty: item.holding,
      collateralCap: tokenInfo?.user_max_qty ?? item.holding,
      collateralRatio: collateralRatio.toNumber(),
      indexPrice: indexPrice,
    });

    return {
      ...item,
      collateralContribution: collateralContribution,
    };
  });

  const currentLtv = useComputedLTV();

  const onConvert = useCallback(async () => {
    return modal.show(isMobile ? "ConvertSheetId" : "ConvertDialogId");
  }, [isMobile]);

  return {
    holdingData,
    isHoldingLoading,
    ltv_threshold: ltvThresholdPercentage,
    negative_usdc_threshold,
    isThresholdLoading,
    currentLtv: currentLtv,
    onConvert: onConvert,
    marginMode,
  };
};

export type LTVTooltipScriptReturn = ReturnType<typeof useLTVTooltipScript>;
