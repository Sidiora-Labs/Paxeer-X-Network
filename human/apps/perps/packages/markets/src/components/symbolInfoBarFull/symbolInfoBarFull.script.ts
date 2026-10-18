import { useEffect, useMemo, useRef, useState } from "react";
import {
  useFundingDetails,
  useFundingRate,
  useGetRwaSymbolInfo,
  useMarketsStore,
  useSymbolsInfo,
  useTickerStream,
} from "@paxeer-network/hooks";
import { Decimal } from "@paxeer-network/utils";

export type UseSymbolInfoBarFullScriptOptions = {
  symbol: string;
};

export type UseSymbolInfoBarFullScriptReturn = ReturnType<
  typeof useSymbolInfoBarFullScript
>;

export function useSymbolInfoBarFullScript(
  options: UseSymbolInfoBarFullScriptOptions,
) {
  const { symbol } = options;

  const data = useTickerStream(symbol);
  const fundingRate = useFundingRate(symbol);

  const favorite = useMarketsStore();

  const { isRwa, open, closeTimeInterval, openTimeInterval } =
    useGetRwaSymbolInfo(symbol);

  const { data: fundingDetails, isLoading: isFundingLoading } =
    useFundingDetails(symbol);

  const fundingPeriod = useMemo(() => {
    if (!fundingDetails || isFundingLoading || !Number.isFinite(fundingDetails.funding_period)) {
      return "-";
    }
    return `${fundingDetails.funding_period}h`;
  }, [fundingDetails, isFundingLoading]);

  const capFunding = useMemo(() => {
    if (!fundingDetails || isFundingLoading || !Number.isFinite(fundingDetails.cap_funding)) {
      return "-";
    }
    return `${new Decimal(fundingDetails.cap_funding).mul(100).toNumber()}%`;
  }, [fundingDetails, isFundingLoading]);

  const floorFunding = useMemo(() => {
    if (!fundingDetails || isFundingLoading || !Number.isFinite(fundingDetails.floor_funding)) {
      return "-";
    }
    return `${new Decimal(fundingDetails.floor_funding).mul(100).toNumber()}%`;
  }, [fundingDetails, isFundingLoading]);

  const info = useSymbolsInfo();
  const quotoDp = info[symbol]("quote_dp");
  const [leadingVisible, setLeadingVisible] = useState(false);
  const [tailingVisible, setTailingVisible] = useState(false);

  const containerRef = useRef<HTMLDivElement>(null);
  const leadingElementRef = useRef<HTMLDivElement>(null);
  const tailingElementRef = useRef<HTMLDivElement>(null);

  const isFavorite = useMemo(
    () => !!favorite.favorites.find((item) => item.name === symbol),
    [favorite.favorites, symbol],
  );

  const openInterest = useMemo(
    () =>
      new Decimal(data?.open_interest ?? 0)
        .mul(data?.index_price ?? 0)
        .toDecimalPlaces(2)
        .valueOf(),
    [data],
  );

  useEffect(() => {
    const intersectionObserver = new IntersectionObserver((entries) => {
      for (let index = 0; index < entries.length; index++) {
        const item = entries[index];
        if (item) {
          if (item.target === leadingElementRef.current) {
            setLeadingVisible(!item.isIntersecting);
          }

          if (item.target === tailingElementRef.current) {
            setTailingVisible(!item.isIntersecting);
          }
        }
      }
    });
    if (leadingElementRef.current) {
      intersectionObserver.observe(leadingElementRef.current);
    }

    if (tailingElementRef.current) {
      intersectionObserver.observe(tailingElementRef.current);
    }

    return () => {
      intersectionObserver.disconnect();
    };
  }, []);

  const onScoll = (direction: string) => {
    containerRef.current?.scrollBy({
      left: direction === "left" ? -100 : 100,
      behavior: "smooth",
    });
  };

  return {
    symbol,
    isFavorite,
    favorite,
    data,
    quotoDp,
    openInterest,
    fundingRate,
    containerRef,
    leadingElementRef,
    tailingElementRef,
    leadingVisible,
    tailingVisible,
    onScoll,
    fundingPeriod,
    capFunding,
    floorFunding,
    isRwa,
    open,
    closeTimeInterval,
    openTimeInterval,
  };
}
