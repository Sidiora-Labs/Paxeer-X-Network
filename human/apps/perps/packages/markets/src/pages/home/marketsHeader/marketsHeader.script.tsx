import { useMemo } from "react";
import { useMarkets, useQuery, MarketsType } from "@paxeer-network/hooks";
import { Decimal } from "@paxeer-network/utils";
import { sortList } from "../../../utils";

export type MarketsHeaderReturns = ReturnType<typeof useMarketsHeaderScript>;

export const useMarketsHeaderScript = () => {
  const data = useDataSource();

  return {
    ...data,
  };
};

export function useDataSource() {
  const [markets, favorite] = useMarkets(MarketsType.ALL);
  const { data: balance } = useQuery("/v1/public/balance/stats");

  const news = useMemo(
    () =>
      sortList(markets, { sortKey: "created_time", sortOrder: "desc" }).slice(
        0,
        5,
      ),
    [markets],
  );

  const gainers = useMemo(
    () =>
      sortList(markets, { sortKey: "change", sortOrder: "desc" }).slice(0, 5),
    [markets],
  );

  const losers = useMemo(
    () =>
      sortList(markets, { sortKey: "change", sortOrder: "asc" }).slice(0, 5),
    [markets],
  );

  const total24Amount = useMemo(
    () =>
      markets?.reduce((prevValue: Decimal, curValue: any) => {
        return prevValue.add(curValue["24h_amount"] || 0);
      }, new Decimal(0)) || new Decimal(0),
    [markets],
  );

  const totalOpenInterest = useMemo(
    () =>
      markets?.reduce((prevValue: Decimal, curValue: any) => {
        return prevValue.add(curValue["openInterest"] || 0);
      }, new Decimal(0)) || new Decimal(0),
    [markets],
  );

  const tvl = useMemo(() => {
    if (!balance) return 0;

    const { total_holding = 0 } = balance as any;
    return new Decimal(total_holding).toNumber();
  }, [balance]);

  return {
    markets,
    news,
    gainers,
    losers,
    total24Amount: total24Amount.toNumber(),
    totalOpenInterest: totalOpenInterest.toNumber(),
    tvl,
    favorite,
  };
}
