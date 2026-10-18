import { useCallback } from "react";
import { useFundingRate } from "@paxeer-network/hooks";
import { modal } from "@paxeer-network/ui";
import { FundingRateSheetId } from "../fundingRateModal/fundingRateModal.widget";

export const useFundingRateScript = (symbol: string) => {
  const data = useFundingRate(symbol);
  const onClick = useCallback<React.MouseEventHandler<HTMLElement>>(() => {
    modal.show(FundingRateSheetId, { symbol });
  }, [symbol]);
  return {
    data,
    onClick,
  };
};

export type FundingRateState = ReturnType<typeof useFundingRateScript>;
