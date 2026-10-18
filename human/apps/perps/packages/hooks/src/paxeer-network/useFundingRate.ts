import { useEffect, useMemo, useRef, useState } from "react";
import { API, SDKError } from "@paxeer-network/types";
import {
  Decimal,
  getTimestamp,
  timeConvertString,
} from "@paxeer-network/utils";
import { useQuery } from "../useQuery";
import { useMarketMap } from "./useMarket/market.store";

export const useFundingRate = (symbol: string) => {
  if (!symbol) {
    throw new SDKError("Symbol is required");
  }

  const [countDown, setCountDown] = useState("00:00:00");

  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const { data: queried, isLoading: queryLoading } = useQuery<API.FundingRate>(
    `/v1/public/funding_rate/${symbol}`,
    {
      fallbackData: {
        est_funding_rate: 0,
        next_funing_time: 0,
      },
    },
  );

  const marketMap = useMarketMap();
  const live = marketMap?.[symbol] as unknown as API.FundingRate | undefined;
  const data = live?.est_funding_rate !== undefined ? live : queried;
  const isLoading = !live && queryLoading;

  useEffect(() => {
    if (!data || isLoading) {
      return;
    }
    const { next_funding_time } = data;
    if (!next_funding_time || next_funding_time <= 0) {
      return;
    }
    timerRef.current = setInterval(() => {
      const diff = new Date(next_funding_time).getTime() - getTimestamp();

      // if diff is less than 0, set count down to 00:00:00
      if (diff <= 0) {
        setCountDown("00:00:00");
        if (timerRef.current) {
          clearInterval(timerRef.current);
        }
        return;
      }

      const result = timeConvertString(diff);
      if (result.length === 3) {
        setCountDown(
          `${result[0].toString().padStart(2, "0")}:${result[1]
            .toString()
            .padStart(2, "0")}:${result[2].toString().padStart(2, "0")}`,
        );
      }
    }, 1000);
    return () => {
      if (timerRef.current) {
        clearInterval(timerRef.current);
      }
    };
  }, [data, isLoading]);

  const est_funding_rate = useMemo(() => {
    if (!data) {
      return;
    }

    const { next_funding_time, est_funding_rate = 0 } = data;

    if (getTimestamp() > next_funding_time) {
      return null;
    }

    return new Decimal(Number(est_funding_rate) * 100).toFixed(
      4,
      Decimal.ROUND_DOWN,
    );
  }, [data]);

  return {
    ...data,
    est_funding_rate,
    countDown,
  };
};
