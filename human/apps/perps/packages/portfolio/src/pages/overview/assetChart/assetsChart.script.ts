import { useMemo } from "react";
import { useAccount } from "@paxeer-network/hooks";
import { useAppContext, useDataTap } from "@paxeer-network/react-app";
import { AccountStatusEnum } from "@paxeer-network/types";
import { useOverviewContext } from "../provider/overviewContext";

export const useAssetsChartScript = () => {
  const assetHistory = useOverviewContext();

  const { wrongNetwork, disabledConnect } = useAppContext();
  const { state } = useAccount();

  const filteredData = useDataTap(assetHistory.data, {
    accountStatus:
      state.status === AccountStatusEnum.EnableTradingWithoutConnected
        ? AccountStatusEnum.EnableTradingWithoutConnected
        : AccountStatusEnum.EnableTrading,
    fallbackData: assetHistory.createFakeData(
      { account_value: 0 },
      { account_value: 500 },
    ),
  });

  const _data = useMemo(() => {
    if (filteredData?.length) {
      return filteredData;
    }
    return assetHistory.createFakeData(
      { account_value: 0 },
      { account_value: 500 },
    );
  }, [filteredData, assetHistory]);

  const invisible =
    wrongNetwork || disabledConnect || !assetHistory.data.length;

  return {
    ...assetHistory,
    data: _data,
    invisible,
  } as const;
};

export type useAssetsChartScriptReturn = ReturnType<
  typeof useAssetsChartScript
>;
