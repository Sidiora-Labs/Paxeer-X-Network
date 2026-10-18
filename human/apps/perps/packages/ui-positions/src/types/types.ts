import { PriceMode } from "@paxeer-network/hooks";
import { API } from "@paxeer-network/types";
import { SortOrder } from "@paxeer-network/ui";
import { SharePnLConfig } from "@paxeer-network/ui-share";

export type SortType = {
  sortKey: string;
  sortOrder: SortOrder;
};

export type PositionsProps = {
  pnlNotionalDecimalPrecision?: number;
  sharePnLConfig?: SharePnLConfig;
  symbol?: string;
  calcMode?: PriceMode;
  includedPendingOrder?: boolean;
  selectedAccount?: string;
  onSymbolChange?: (symbol: API.Symbol) => void;
  enableSortingStorage?: boolean; // Controls whether to persist sorting preferences
};
