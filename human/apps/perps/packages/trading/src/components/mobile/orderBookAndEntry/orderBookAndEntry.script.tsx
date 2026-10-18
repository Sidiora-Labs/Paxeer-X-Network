import { useLocalStorage } from "@paxeer-network/hooks";
import { useTradingPageContext } from "../../../provider/tradingPageContext";

const PAXEER_NETWORK_MWEB_ORDER_ENTRY_SIDE_MARKETS_LAYOUT =
  "paxeerNetwork_mweb_order_entry_side_markets_layout";

export type OrderBookAndEntryLayout = "left" | "right";

export const useOrderBookAndEntryScript = () => {
  const { symbol } = useTradingPageContext();
  const [layout] = useLocalStorage<OrderBookAndEntryLayout>(
    PAXEER_NETWORK_MWEB_ORDER_ENTRY_SIDE_MARKETS_LAYOUT,
    "right",
  );
  return {
    symbol,
    layout,
  };
};

export type OrderBookAndEntryState = ReturnType<
  typeof useOrderBookAndEntryScript
>;
