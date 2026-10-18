import { useMemo } from "react";
import { useSymbolsInfo } from "./paxeer-network/useSymbolsInfo";
import { useAllBrokers } from "./trading-rewards/useAllBrokers";

export function getSymbolBase(symbol: string | undefined): string {
  if (!symbol) return "";
  const parts = symbol.split("_");
  return parts.length >= 3
    ? (parts[1] ?? "")
    : (symbol.match(/^([A-Za-z]+)/)?.[1] ?? symbol);
}

export function getSymbolDisplayName(
  symbol: string | undefined,
  displaySymbolName?: string | null,
): string {
  const trimmedDisplayName = displaySymbolName?.trim();
  if (trimmedDisplayName) return trimmedDisplayName;
  return getSymbolBase(symbol);
}

/**
 * First segment of raw broker name when split by space, hyphen, or underscore
 * (e.g. ` paxeer-network Agent`, `paxeer-network-Agent`, `PaxeerNetwork_Agent` → `paxeer-network`). No length truncation.
 */
function brokerNameBaseFromRaw(
  rawBrokerName: string | undefined,
): string | undefined {
  if (!rawBrokerName) return undefined;
  const first = rawBrokerName.trim().split(/[ _-]/, 1)[0]?.trim() ?? "";
  return first.length > 0 ? first : undefined;
}

export interface UseBadgeBySymbolReturn {
  /** Display name of the symbol. API.display_symbol_name */
  displaySymbolName: string;
  /** Broker ID of the symbol. */
  brokerId: string | undefined;
  /** Badge label: first segment of raw name, truncated to 7 chars with "...". */
  brokerName: string | undefined;
  /** Raw broker name as provided by the API. */
  brokerNameRaw: string | undefined;
}

/**
 * Match the given `symbol` in `symbolsInfo` and return `displaySymbolName`,
 * `brokerId`, and the mapped `brokerName`.
 *
 * `brokerName` comes from the `/v1/public/broker/name` broker list
 * (SWR shared cache).
 */
export const useBadgeBySymbol = (symbol: string): UseBadgeBySymbolReturn => {
  const symbolsInfo = useSymbolsInfo();
  const [brokers] = useAllBrokers();

  return useMemo(() => {
    if (!symbol || symbolsInfo.isNil) {
      return {
        displaySymbolName: symbol ?? "",
        brokerId: undefined,
        brokerName: undefined,
        brokerNameRaw: undefined,
      };
    }

    const getter = symbolsInfo[symbol];
    const info = typeof getter === "function" ? getter() : undefined;

    const displaySymbolName = getSymbolDisplayName(
      symbol,
      (info as { display_symbol_name?: string } | undefined)
        ?.display_symbol_name,
    );

    const brokerId: string | undefined = info?.broker_id ?? undefined;
    const rawBrokerName = brokerId ? brokers?.[brokerId] : undefined;
    const base = brokerNameBaseFromRaw(rawBrokerName);
    const brokerName = base
      ? base.length > 7
        ? `${base.slice(0, 7)}...`
        : base
      : undefined;

    return {
      displaySymbolName,
      brokerId,
      brokerName,
      brokerNameRaw: rawBrokerName,
    };
  }, [brokers, symbolsInfo, symbol]);
};

/**
 * Same rules as {@link useSymbolWithBroker}, for use in callbacks / non-React code paths.
 */
export function formatSymbolWithBroker(
  symbol: string,
  symbolsInfo: { isNil?: boolean; [key: string]: unknown },
  brokers: Record<string, string> | undefined,
): string {
  if (!symbol) return "";

  let brokerNameBase: string | undefined;
  if (!symbolsInfo.isNil) {
    const getter = symbolsInfo[symbol] as
      | (() => { broker_id?: string })
      | undefined;
    const info = typeof getter === "function" ? getter() : undefined;
    const brokerId: string | undefined = info?.broker_id ?? undefined;
    const rawBrokerName = brokerId ? brokers?.[brokerId] : undefined;
    brokerNameBase = brokerNameBaseFromRaw(rawBrokerName);
  }

  const base = getSymbolBase(symbol);

  const hasBrokerSuffix = symbol.includes("-") || symbol.split("_").length > 3;

  if (brokerNameBase && hasBrokerSuffix) {
    return `${base}-${brokerNameBase}`;
  }
  return base;
}

/**
 * Short market label: `base`, or `base-{brokerNameBase}` when the symbol is broker-scoped and a broker
 * name is available from symbols info + broker list.
 *
 * Symbol shape: `type_base_quote` with optional trailing `_broker_id` segments (broker_id may
 * contain underscores, e.g. `paxeer-network`). Otherwise falls back to a leading letter run for legacy
 * compact symbols.
 */
export const useSymbolWithBroker = (symbol: string): string => {
  const symbolsInfo = useSymbolsInfo();
  const [brokers] = useAllBrokers();

  return useMemo(
    () => formatSymbolWithBroker(symbol, symbolsInfo, brokers),
    [brokers, symbolsInfo, symbol],
  );
};
