import { useMemo } from "react";
import { useAccount, useLocalStorage } from "@paxeer-network/hooks";
import { useAppContext, useDataTap } from "@paxeer-network/react-app";
import { AccountStatusEnum } from "@paxeer-network/types";
import { PAXEER_NETWORK_ASSETS_VISIBLE_KEY } from "../../assets/type";
import { useOverviewContext } from "../provider/overviewContext";

export const usePerformanceScript = () => {
  const ctx = useOverviewContext();
  const [visible, setVisible] = useLocalStorage<boolean>(
    PAXEER_NETWORK_ASSETS_VISIBLE_KEY,
    true,
  );

  const { wrongNetwork, disabledConnect } = useAppContext();
  const { state } = useAccount();
  const filteredData = useDataTap(ctx?.data, {
    accountStatus:
      state.status === AccountStatusEnum.EnableTradingWithoutConnected
        ? AccountStatusEnum.EnableTradingWithoutConnected
        : AccountStatusEnum.EnableTrading,
    fallbackData: ctx?.createFakeData?.(
      { account_value: 0, pnl: 0 },
      { account_value: 500, pnl: 500 },
    ),
  });

  const _data = useMemo(() => {
    if (filteredData?.length) {
      return filteredData;
    }
    return ctx?.createFakeData?.(
      { account_value: 0, pnl: 0 },
      { account_value: 500, pnl: 500 },
    );
  }, [ctx, filteredData]);

  const invisible =
    wrongNetwork ||
    disabledConnect ||
    (state.status < AccountStatusEnum.EnableTrading &&
      state.status !== AccountStatusEnum.EnableTradingWithoutConnected);

  return {
    ...ctx,
    data: _data as ReadonlyArray<any>,
    invisible,
    visible: visible as boolean,
    setVisible: setVisible,
  };
};

export type UsePerformanceScriptReturn = ReturnType<
  typeof usePerformanceScript
>;
