import { useState } from "react";
import { useOrderStream } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { modal, Text } from "@paxeer-network/ui";
import { TabType } from "@paxeer-network/ui-orders";
import { SharePnLConfig } from "@paxeer-network/ui-share";
import { formatSymbol } from "@paxeer-network/utils";
import {
  usePendingOrderCount,
  usePositionsCount,
  useTradingLocalStorage,
} from "../../../hooks";
import { useTradingPageContext } from "../../../provider/tradingPageContext";

export enum DataListTabType {
  position = "Position",
  pending = "Pending",
  tp_sl = "TP/SL",
  history = "History",
  liquidation = "Liquidation",
  assets = "Assets",
}

export enum DataListTabSubType {
  positionHistory = "Position history",
  orderHistory = "Order history",
}

export const useDataListScript = (props: {
  symbol: string;
  className?: string;
  sharePnLConfig?: SharePnLConfig;
}) => {
  const { symbol, sharePnLConfig } = props;
  const [tab, setTab] = useState<DataListTabType>(DataListTabType.position);
  const [subTab, setSubTab] = useState<DataListTabSubType>(
    DataListTabSubType.positionHistory,
  );
  const { t } = useTranslation();

  const { onSymbolChange } = useTradingPageContext();
  const localStorage = useTradingLocalStorage();

  const [_, { cancelAllPendingOrders, cancelAllTPSLOrders }] = useOrderStream(
    {},
  );
  const { positionCount } = usePositionsCount(symbol);
  const { pendingOrderCount, tpSlOrderCount } = usePendingOrderCount(symbol);

  const onCloseAll = (type: TabType) => {
    const { title, content } = getDialogInfo(
      type,
      t,
      localStorage.showAllSymbol ? undefined : symbol,
    );
    modal.confirm({
      title: title,
      content: <Text size="2xs">{content}</Text>,

      onOk: async () => {
        try {
          // await cancelAll(null, { source_type: "ALL" });
          if (tab === DataListTabType.tp_sl) {
            await cancelAllTPSLOrders(
              localStorage.showAllSymbol ? undefined : symbol,
            );
          } else {
            await cancelAllPendingOrders(
              localStorage.showAllSymbol ? undefined : symbol,
            );
          }
          return Promise.resolve(true);
        } catch (error) {
          // @ts-ignore
          if (error?.message !== undefined) {
            // @ts-ignore
            toast.error(error.message);
          }
          return Promise.resolve(false);
        } finally {
          Promise.resolve();
        }
      },
    });
  };

  return {
    tab,
    setTab,
    subTab,
    setSubTab,
    sharePnLConfig,
    symbol,
    positionCount,
    pendingOrderCount,
    tpSlOrderCount,
    ...localStorage,
    onCloseAll,
    onSymbolChange,
  };
};

export type DataListState = ReturnType<typeof useDataListScript>;

type TranslationFn = (...args: any[]) => string;

const getDialogInfo = (type: TabType, t: TranslationFn, symbol?: string) => {
  // symbol like this: PERP_BTC_USDC, but i want to show BTC, pls help me to format the symbol
  const formattedSymbol = symbol ? formatSymbol(symbol, "base") : symbol;
  switch (type) {
    case TabType.pending:
      if (symbol !== undefined) {
        return {
          title: t("orders.pending.cancelAll.forSymbol", {
            symbol: formattedSymbol,
          }),
          content: t("orders.pending.cancelAll.forSymbol.description", {
            symbol: formattedSymbol,
          }),
        };
      }
      return {
        title: t("orders.pending.cancelAll"),
        content: t("orders.pending.cancelAll.description"),
      };
    case TabType.tp_sl:
      if (symbol !== undefined) {
        return {
          title: t("orders.tpsl.cancelAll.forSymbol", {
            symbol: formattedSymbol,
          }),
          content: t("orders.tpsl.cancelAll.forSymbol.description", {
            symbol: formattedSymbol,
          }),
        };
      }
      return {
        title: t("orders.tpsl.cancelAll"),
        content: t("orders.tpsl.cancelAll.description"),
      };
    default:
      return {
        title: "",
        content: "",
      };
  }
};
