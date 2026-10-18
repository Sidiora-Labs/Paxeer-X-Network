import { useMemo } from "react";
import { useRef } from "react";
import {
  MarketsType,
  useAllBrokers,
  useConfig,
  useMarkets,
} from "@paxeer-network/hooks";

export type CommunityTabItem = {
  id: string;
  label: string;
};

export function createCommunityBrokerFilter(selected: string) {
  return (data: any[]) =>
    selected === "all"
      ? data?.filter((m) => Boolean(m?.broker_id))
      : data?.filter((m) => m?.broker_id === selected);
}

/** Compatible with market items that may contain broker_id (e.g. MarketsItem) */
type MarketLike = Record<string, unknown> & { broker_id?: string };

/**
 * Build community sub-tabs from markets and broker mapping.
 * Sorted by broker name/id, and pin current broker to the top.
 *
 * Data sources: useMarkets(ALL), useAllBrokers, useConfig("brokerId").
 */
export function useCommunityTabs(): CommunityTabItem[] {
  const brokerId = useConfig("brokerId") as string | undefined;
  const [brokers] = useAllBrokers();
  const [allMarkets] = useMarkets(MarketsType.ALL);

  const lastSignatureRef = useRef<string>("");
  const lastValueRef = useRef<CommunityTabItem[]>([]);

  return useMemo(() => {
    const brokerIdSet = new Set<string>();
    for (const m of allMarkets ?? []) {
      const id = (m as MarketLike)?.broker_id;
      if (typeof id === "string" && id.length) {
        brokerIdSet.add(id);
      }
    }

    const entries: Array<{ id: string; name?: string }> = Array.from(
      brokerIdSet,
    ).map((id) => ({
      id,
      name: brokers?.[id],
    }));

    const sorted = entries
      .map(({ id, name }) => ({
        id,
        label: name ?? id,
        sortKey: (name ?? id).toLowerCase(),
      }))
      .sort((a, b) => a.sortKey.localeCompare(b.sortKey));

    if (brokerId) {
      const idx = sorted.findIndex((x) => x.id === brokerId);
      if (idx > 0) {
        const [cur] = sorted.splice(idx, 1);
        sorted.unshift(cur);
      }
    }

    // allMarkets can refresh frequently (reference changes). Reuse the previous
    // array instance when tabs are logically unchanged to avoid downstream re-renders.
    const signature = sorted.map((x) => `${x.id}:${x.label}`).join("|");
    if (signature === lastSignatureRef.current) {
      return lastValueRef.current;
    }
    lastSignatureRef.current = signature;
    lastValueRef.current = sorted;
    return sorted;
  }, [allMarkets, brokers, brokerId]);
}
