import { useMemo } from "react";
import { useComputedLTV, useConvertThreshold } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";

export const useWithdrawLTV = (params: { token: string; quantity: string }) => {
  const { token, quantity } = params;
  const { ltv_threshold } = useConvertThreshold();
  const { t } = useTranslation();

  const currentLTV = useComputedLTV();

  const nextLTV = useComputedLTV({
    input: quantity ? -Number(quantity) : 0,
    token,
  });

  const ltvWarningMessage = useMemo(() => {
    if (ltv_threshold && nextLTV) {
      const threshold = ltv_threshold * 100;
      if (nextLTV > threshold) {
        return t("transfer.withdraw.LTV.error", {
          threshold,
        });
      }
    }
    return "";
  }, [ltv_threshold, nextLTV, t]);

  return {
    currentLTV,
    nextLTV,
    ltvWarningMessage,
    t,
  };
};
