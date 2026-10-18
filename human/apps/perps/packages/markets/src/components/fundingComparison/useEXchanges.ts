import { useMemo } from "react";
import { useMarketsContext } from "../marketsProvider";

export const useEXchanges = () => {
  const { comparisonProps } = useMarketsContext();
  const brokerName = comparisonProps?.exchangesName || "paxeer-network";
  const brokerIconSrc =
    comparisonProps?.exchangesIconSrc ||
    "/assets/static/exchange_logo/paxeer-network.png";
  const exchanges = useMemo<string[]>(() => {
    return [
      brokerName,
      "Binance",
      `${brokerName} - Binance`,
      "OKX",
      `${brokerName} - OKX`,
      "Bybit",
      `${brokerName} - Bybit`,
      "dYdX",
      `${brokerName} - dYdX`,
      "Bitget",
      `${brokerName} - Bitget`,
      "KuCoin",
      `${brokerName} - KuCoin`,
    ];
  }, [comparisonProps?.exchangesName]);
  return {
    exchanges,
    brokerName,
    brokerIconSrc: brokerIconSrc,
  };
};
