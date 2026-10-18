import { useMemo } from "react";
import {
  useFundingDetails,
  useFundingRateBySymbol,
  usePositionStream,
} from "@paxeer-network/hooks";
import { Decimal } from "@paxeer-network/utils";

const TIMEFRAME_CONFIG = [
  { key: "1H", hours: 1 },
  { key: "4H", hours: 4 },
  { key: "1D", hours: 24 },
  { key: "7D", hours: 168 },
  { key: "30D", hours: 720 },
  { key: "1Y", hours: 8760 },
] as const;

interface FundingRateOptions {
  symbol: string;
}

export const useFundingRateModalScript = (options: FundingRateOptions) => {
  const { data, isLoading } = useFundingDetails(options.symbol);
  const {
    last_funding_rate,
    est_funding_rate,
    next_funding_time,
    last_funding_rate_timestamp,
  } = useFundingRateBySymbol(options.symbol) ?? {};

  const [{ aggregated, rows }] = usePositionStream(options.symbol);

  const { notional } = aggregated ?? {};

  const fundingPeriod = useMemo(() => {
    if (!data || isLoading) {
      return "-";
    }
    return `${data.funding_period}h`;
  }, [data, isLoading]);

  const capFunding = useMemo(() => {
    if (!data || isLoading) {
      return "-";
    }
    return `${new Decimal(data.cap_funding).mul(100).toNumber()}%`;
  }, [data, isLoading]);

  const floorFunding = useMemo(() => {
    if (!data || isLoading) {
      return "-";
    }
    return `${new Decimal(data.floor_funding).mul(100).toNumber()}%`;
  }, [data, isLoading]);

  const lastFundingRate = useMemo(() => {
    if (!last_funding_rate) {
      return undefined;
    }
    return `${new Decimal(last_funding_rate).mul(100).toNumber()}%`;
  }, [last_funding_rate]);

  const estFundingFee = useMemo(() => {
    if (!est_funding_rate || !notional || rows.length === 0) {
      return "--";
    }

    return `${new Decimal(est_funding_rate).mul(notional).todp(4).toNumber()}`;
  }, [est_funding_rate, notional, rows]);

  // Calculate annualized funding rate
  const calculateAnnualizedRate = (rate: number, intervalHours: number) => {
    if (!rate || !intervalHours) return undefined;
    // annualized rate = funding rate * 24h / interval(h) * 365
    const annualizedRate = new Decimal(rate)
      .mul(24)
      .div(intervalHours)
      .mul(365);
    // Round down to two decimal places
    return annualizedRate.todp(2, Decimal.ROUND_DOWN).toNumber();
  };

  const lastFundingRateAnnualized = useMemo(() => {
    if (!last_funding_rate || !data?.funding_period) {
      return undefined;
    }
    const rate = new Decimal(last_funding_rate).mul(100).toNumber();
    const annualized = calculateAnnualizedRate(rate, data.funding_period);
    return annualized ? `${annualized}%` : undefined;
  }, [last_funding_rate, data?.funding_period]);

  const settlementIntervalHours = useMemo(() => {
    if (!next_funding_time || !last_funding_rate_timestamp) return 8;
    const diff = next_funding_time - last_funding_rate_timestamp;
    return diff / 3_600_000;
  }, [next_funding_time, last_funding_rate_timestamp]);

  const estFundingRateByTimeframe = useMemo(() => {
    if (
      est_funding_rate === undefined ||
      est_funding_rate === null ||
      settlementIntervalHours <= 0
    ) {
      return [];
    }
    return TIMEFRAME_CONFIG.map(({ key, hours }) => {
      const rate = new Decimal(est_funding_rate)
        .mul(hours)
        .div(settlementIntervalHours)
        .mul(100);
      const str = rate.toFixed(5);
      const trimmed = str.replace(/\.?0+$/, "") || "0";
      return { timeframe: key, value: `${trimmed}%` };
    });
  }, [est_funding_rate, settlementIntervalHours]);

  return {
    fundingPeriod,
    capFunding,
    floorFunding,
    lastFundingRate,
    estFundingFee,
    lastFundingRateAnnualized,
    estFundingRateByTimeframe,
  };
};

export type FundingRateModalState = ReturnType<
  typeof useFundingRateModalScript
>;
