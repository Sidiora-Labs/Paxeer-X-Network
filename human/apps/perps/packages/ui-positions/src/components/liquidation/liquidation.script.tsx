import { useEffect, useMemo, useState } from "react";
import { differenceInDays, subDays } from "date-fns";
import {
  usePrivateInfiniteQuery,
  usePrivateQuery,
} from "@paxeer-network/hooks";
import { useDataTap } from "@paxeer-network/react-app";
import { API } from "@paxeer-network/types";
import { usePagination, useScreen } from "@paxeer-network/ui";
import { Decimal, zero } from "@paxeer-network/utils";
import {
  areDatesEqual,
  formatDatePickerRange,
  offsetEndOfDay,
  offsetStartOfDay,
} from "../../utils";
import { LiquidationProps } from "./liquidation.widget";

export const useLiquidationScript = (props: LiquidationProps) => {
  const { symbol, enableLoadMore } = props;
  const { page, pageSize, setPage, pagination, parsePagination } =
    usePagination({
      pageSize: 10,
    });

  const { dateRange, filterDays, updateFilterDays, filterItems, onFilter } =
    useFilter();

  useEffect(() => {
    setPage(1);
  }, [symbol, dateRange, filterDays]);

  const [data, { meta, isLoading, loadMore }] = useLiquidation({
    page: enableLoadMore ? undefined : page,
    size: pageSize,
    symbol,
    start_t: dateRange.from != null ? dateRange.from.getTime() : undefined,
    end_t: dateRange.to != null ? dateRange.to.getTime() : undefined,
  });

  const dataSource = useDataTap(data, {
    fallbackData: [],
  });

  // useEffect(() => {
  //   setPage(1);
  // }, [dateRange, filterDays]);

  // console.log("pagination", pagination, meta);

  return {
    dataSource,
    isLoading,
    loadMore,

    pagination: parsePagination(meta),

    // filter
    dateRange,
    filterDays,
    updateFilterDays,
    filterItems,
    onFilter,
  };
};

export type LiquidationState = ReturnType<typeof useLiquidationScript>;

/**
 *
 * @param props size default is 100
 */
const useLiquidation = (props: {
  symbol?: string;
  start_t?: number;
  end_t?: number;
  page?: number;
  size?: number;
}) => {
  const ordersResponse = usePrivateInfiniteQuery(generateKeyFun(props), {
    initialSize: 1,
    formatter: (data) => data,
    revalidateOnFocus: true,
  });

  const meta = useMemo(() => {
    // @ts-ignore
    return ordersResponse.data?.[0]?.meta;
  }, [ordersResponse.data?.[0]]);

  const data = useMemo(() => {
    return ordersResponse.data
      ?.map((item) =>
        // @ts-ignore
        item.rows?.map((item) => {
          let liquidationFeeRate = null;
          const firstPosition = item.positions_by_perp[0];

          if (firstPosition) {
            liquidationFeeRate = new Decimal(firstPosition.liquidator_fee)
              .add(firstPosition.insurance_fund_fee)
              .toNumber();
          }

          return {
            ...item,
            formatted_account_mmr: isNaN(item.account_mmr)
              ? null
              : new Decimal(item.account_mmr).mul(100).toFixed(2).toString(),
            liquidationFeeRate,
          };
        }),
      )
      ?.flat();
  }, [ordersResponse.data]);

  const isLoading = ordersResponse.isLoading;

  const loadMore = () => {
    ordersResponse.setSize(ordersResponse.size + 1);
  };

  return [data, { meta, isLoading, loadMore }] as const;
};

const useFilter = () => {
  const defaultRange = formatDatePickerRange({
    to: offsetEndOfDay(new Date()),
    from: offsetStartOfDay(subDays(new Date(), 89)),
  });

  /// default is 90d
  const [filterDays, setFilterDays] = useState<1 | 7 | 30 | 90 | null>(90);

  const [dateRange, setDateRange] = useState<{
    from?: Date;
    to?: Date;
  }>(defaultRange);

  const updateFilterDays = (days: 1 | 7 | 30 | 90) => {
    setFilterDays(days);
    setDateRange({
      from: offsetStartOfDay(subDays(new Date(), days - 1)),
      to: offsetEndOfDay(new Date()),
    });
  };

  const onFilter = (filter: { name: string; value: any }) => {
    if (filter.name === "dateRange") {
      const newDateRange = formatDatePickerRange(filter.value);
      setDateRange(newDateRange);
      if (newDateRange.from && newDateRange.to) {
        const diffDays =
          Math.abs(differenceInDays(newDateRange.from, newDateRange.to)) + 1;
        const dateRangeMap: { [key: number]: { from: Date; to: Date } } = {
          1: {
            from: offsetStartOfDay(new Date())!,
            to: offsetEndOfDay(new Date())!,
          },
          7: {
            from: offsetStartOfDay(subDays(new Date(), 6))!,
            to: offsetEndOfDay(new Date())!,
          },
          30: {
            from: offsetStartOfDay(subDays(new Date(), 29))!,
            to: offsetEndOfDay(new Date())!,
          },
          90: {
            from: offsetStartOfDay(subDays(new Date(), 89))!,
            to: offsetEndOfDay(new Date())!,
          },
        };

        const dateRange = dateRangeMap[diffDays];
        if (
          dateRange &&
          areDatesEqual(dateRange.from, newDateRange.from) &&
          areDatesEqual(dateRange.to, newDateRange.to)
        ) {
          setFilterDays(diffDays as any);
        } else {
          setFilterDays(null);
        }
      }
    }
  };

  const { isMobile } = useScreen();

  const filterItems = useMemo((): any[] => {
    const dateRangeFilter = {
      type: "range",
      name: "dateRange",
      value: dateRange,
      fromDate: offsetStartOfDay(subDays(new Date(), 89)),
      toDate: offsetEndOfDay(new Date()),
    };

    if (isMobile) {
      return [dateRangeFilter];
    }
    return [dateRangeFilter];
  }, [dateRange]);

  return {
    filterItems,
    onFilter,
    dateRange,
    filterDays,
    updateFilterDays,
  };
};

const generateKeyFun =
  (args: {
    symbol?: string;
    start_t?: number;
    end_t?: number;
    page?: number;
    size?: number;
  }) =>
  (pageIndex: number, previousPageData: any): string | null => {
    // reached the end
    if (previousPageData && !previousPageData.rows?.length) return null;

    const { symbol, size = 10, page, end_t, start_t } = args;

    const search = new URLSearchParams([
      ["size", size.toString()],
      ["page", `${pageIndex + 1}`],
    ]);

    if (page) {
      search.set("page", `${page}`);
    }

    if (symbol) {
      search.set(`symbol`, symbol);
    }

    if (end_t) {
      search.set(`end_t`, `${end_t}`);
    }

    if (start_t) {
      search.set(`start_t`, `${start_t}`);
    }

    return `/v1/liquidations?${search.toString()}`;
  };
