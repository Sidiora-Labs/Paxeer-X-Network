import { useMemo } from "react";
import { useOrderStream } from "@paxeer-network/hooks";
import { useDataTap } from "@paxeer-network/react-app";
import { AlgoOrderRootType, OrderStatus } from "@paxeer-network/types";
import { useTradingLocalStorage } from "./useTradingLocalStorage";

export const usePendingOrderCount = (symbol?: string) => {
  const { showAllSymbol } = useTradingLocalStorage();

  // const pendingOrdersPageSizeKey = `paxeerNetwork_${TabType.pending}_pageSize`;
  // const tpslOrdersPageSizeKey = `paxeerNetwork_${TabType.tp_sl}_pageSize`;

  // const [pendingOrderPageSize] = useLocalStorage(pendingOrdersPageSizeKey, 500);
  // const [tpslOrderPageSize] = useLocalStorage(tpslOrdersPageSizeKey, 500);

  const [pendingOrders] = useOrderStream(
    {
      symbol: showAllSymbol ? undefined : symbol,
      status: OrderStatus.INCOMPLETE,
      excludes: [AlgoOrderRootType.POSITIONAL_TP_SL, AlgoOrderRootType.TP_SL],
      // size: pendingOrderPageSize,
      size: 500,
      // sourceTypeAll: true,
    },
    {
      keeplive: true,
    },
  );

  const [tpslOrders] = useOrderStream(
    {
      symbol: showAllSymbol ? undefined : symbol,
      status: OrderStatus.INCOMPLETE,
      includes: [AlgoOrderRootType.POSITIONAL_TP_SL, AlgoOrderRootType.TP_SL],
      // size: tpslOrderPageSize,
      size: 500,
      // sourceTypeAll: true,
    },
    {
      keeplive: true,
    },
  );

  const pendingCount = useMemo(() => {
    if (showAllSymbol) {
      return pendingOrders?.length ?? 0;
    }
    return pendingOrders?.filter((item) => item.symbol === symbol)?.length ?? 0;
  }, [pendingOrders, showAllSymbol, symbol]);

  const tpslCount = useMemo(() => {
    if (showAllSymbol) {
      return tpslOrders?.length ?? 0;
    }
    return tpslOrders?.filter((item) => item.symbol === symbol)?.length ?? 0;
  }, [tpslOrders, showAllSymbol, symbol]);

  const pendingOrderCount = useDataTap(pendingCount) ?? 0;
  const tpSlOrderCount = useDataTap(tpslCount) ?? 0;

  return {
    pendingOrderCount,
    tpSlOrderCount,
  };
};
