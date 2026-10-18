import { useMemo } from "react";
import {
  useAccount,
  useLeverage,
  useMarginRatio,
} from "@paxeer-network/hooks";
import { useDataTap } from "@paxeer-network/react-app";
import { AccountStatusEnum } from "@paxeer-network/types";
import { Decimal } from "@paxeer-network/utils";

const getRiskRateColor = (riskRate: null | number) => {
  if (riskRate === null) {
    return { isHigh: false, isMedium: false, isLow: false, isDefault: true };
  }

  const isLow = riskRate < 40;
  const isMedium = riskRate >= 40 && riskRate < 80;
  const isHigh = riskRate >= 80;
  const isDefault = !isLow && !isMedium && !isHigh;

  return { isHigh, isMedium, isLow, isDefault };
};

export const useRiskRateScript = () => {
  const { state } = useAccount();

  const isConnected =
    state.status >= AccountStatusEnum.EnableTrading ||
    state.status === AccountStatusEnum.EnableTradingWithoutConnected;
  const { marginRatio, currentLeverage, mmr } = useMarginRatio();

  const { curLeverage } = useLeverage();

  const riskRate = useMemo(() => {
    if (!isConnected || marginRatio === null || mmr === null) {
      return "--";
    }

    if (marginRatio === 0 || mmr === 0) {
      return "0%";
    }

    const calculatedRiskRate = new Decimal(mmr)
      .div(marginRatio)
      .mul(100)
      .todp(2, Decimal.ROUND_UP);

    // Remove trailing zeroes and add percentage sign
    return `${calculatedRiskRate.toString().replace(/\.?0+$/, "")}%`;
  }, [isConnected, marginRatio, mmr]);

  const riskRateNumber = riskRate === "--" ? null : parseFloat(riskRate);

  const riskRateColor = useMemo(() => {
    return getRiskRateColor(riskRateNumber);
  }, [riskRateNumber]);

  const _curLeverage = useDataTap(currentLeverage);
  const _maxLeverage = useDataTap(curLeverage);
  const _riskRate = useDataTap(riskRate);

  return {
    riskRate: _riskRate,
    riskRateColor,
    isConnected,
    currentLeverage: _curLeverage,
    maxLeverage: _maxLeverage,
  };
};

export type RiskRateState = ReturnType<typeof useRiskRateScript>;
