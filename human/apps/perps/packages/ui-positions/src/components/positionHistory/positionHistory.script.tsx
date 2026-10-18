import { useEffect, useMemo, useState } from "react";
import { differenceInDays, subDays } from "date-fns";
import { useAccount, usePrivateQuery } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { useDataTap } from "@paxeer-network/react-app";
import { AccountStatusEnum, MarginMode } from "@paxeer-network/types";
import { API } from "@paxeer-network/types";
import { usePagination, useScreen } from "@paxeer-network/ui";
import { TRADING_POSITIONS_SORT_STORAGE_KEY } from "../../constants";
import {
  areDatesEqual,
  formatDatePickerRange,
  offsetEndOfDay,
  offsetStartOfDay,
} from "../../utils";
import { useSort } from "../../utils/sorting";
import { useTabSort, PositionsTabName } from "../shared/hooks/useTabSort";
import { PositionHistoryProps } from "./positionHistory.widget";

export type PositionHistoryExt = API.PositionHistory & {
  netPnL?: number;
};

export enum PositionHistorySide {
  all = "all",
  buy = "LONG",
  sell = "SHORT",
}
export enum PositionHistoryStatus {
  all = "all",
  closed = "closed",
  partial_closed = "partial_closed",
}

const normalizeMarginMode = (
  value?: API.PositionHistory["margin_mode"] | null,
): MarginMode | undefined => {
  if (value === 1 || value === MarginMode.ISOLATED) {
    return MarginMode.ISOLATED;
  }

  if (value === 0 || value === MarginMode.CROSS) {
    return MarginMode.CROSS;
  }

  return undefined;
};

export const usePositionHistoryScript = (props: PositionHistoryProps) => {
  const {
    onSymbolChange,
    symbol,
    pnlNotionalDecimalPrecision,
    enableSortingStorage = true,
  } = props;
  const { state } = useAccount();

  // Sorting functionality
  const { tabSort, onTabSort } = useTabSort({
    storageKey: TRADING_POSITIONS_SORT_STORAGE_KEY,
  });

  const { onSort, getSortedList } = useSort(
    enableSortingStorage
      ? tabSort?.[PositionsTabName.PositionHistory]
      : undefined,
    enableSortingStorage
      ? onTabSort(PositionsTabName.PositionHistory)
      : undefined,
  );

  const { data, isLoading } = usePrivateQuery<PositionHistoryExt[]>(
    symbol
      ? `/v1/position_history?symbol=${symbol}&limit=1000`
      : "/v1/position_history?limit=1000",
    {
      formatter(data) {
        return (data.rows ?? null)?.map((item: API.PositionHistory) => {
          const result: PositionHistoryExt = {
            ...item,
            margin_mode: normalizeMarginMode(item.margin_mode),
          };

          if (
            item.realized_pnl != null &&
            item.accumulated_funding_fee != null &&
            item.trading_fee != null
          ) {
            result.netPnL =
              item.realized_pnl -
              item.accumulated_funding_fee -
              item.trading_fee;
          }

          return result;
        });
      },
      revalidateOnFocus: true,
    },
  );

  const { pagination, setPage } = usePagination({
    pageSize: 10,
  });

  const {
    status,
    side,
    dateRange,
    filterDays,
    updateFilterDays,
    filterItems,
    onFilter,
  } = useFilter();

  useEffect(() => {
    setPage(1);
  }, [status, side, dateRange, filterDays, symbol]);

  const filterData = useMemo(() => {
    if (data == null) return data;

    return data.filter((item) => {
      const sideFilter =
        side === PositionHistorySide.all
          ? true
          : item.side.toLowerCase() === side.toLowerCase();
      const statusFilter =
        status === PositionHistoryStatus.all
          ? true
          : item.position_status.toLowerCase() === status.toLowerCase();

      // const time = item.close_timestamp ?? item?.last_update_timestamp ?? item.open_timestamp;
      const time = item?.last_update_time ?? item.open_timestamp;
      const dateFilter =
        dateRange.from && dateRange.to
          ? time >= dateRange.from.getTime() && time <= dateRange.to.getTime()
          : true;

      const symbolFilter = symbol ? item.symbol == symbol : true;

      return sideFilter && statusFilter && dateFilter && symbolFilter;
    });
  }, [status, side, dateRange, data, symbol]);

  const rawDataSource = useDataTap(filterData, {
    accountStatus:
      state.status === AccountStatusEnum.EnableTradingWithoutConnected
        ? AccountStatusEnum.EnableTradingWithoutConnected
        : AccountStatusEnum.EnableTrading,
    fallbackData: [],
  });

  const dataSource = getSortedList(rawDataSource || []);

  return {
    dataSource,
    isLoading,
    onSymbolChange,
    pagination,
    filterItems,
    onFilter,
    symbol,
    filterDays,
    updateFilterDays,
    pnlNotionalDecimalPrecision,
    onSort,
    initialSort: enableSortingStorage
      ? tabSort?.[PositionsTabName.PositionHistory]
      : undefined,
  };
};

const useFilter = () => {
  const { t } = useTranslation();

  const [status, setStatus] = useState<PositionHistoryStatus>(
    PositionHistoryStatus.all,
  );
  const [side, setSide] = useState<PositionHistorySide>(
    PositionHistorySide.all,
  );
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
    if (filter.name === "side") {
      setSide(filter.value);
    }

    if (filter.name === "status") {
      setStatus(filter.value);
    }

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
    const sideFilter = {
      type: "select",
      name: "side",
      options: [
        {
          label: t("common.side.all"),
          value: "all",
        },
        {
          label: t("common.buy"),
          value: PositionHistorySide.buy,
        },
        {
          label: t("common.sell"),
          value: PositionHistorySide.sell,
        },
      ],
      value: side,
    };

    const dateRangeFilter = {
      type: "range",
      name: "dateRange",
      value: dateRange,
      fromDate: offsetStartOfDay(subDays(new Date(), 89)),
      toDate: offsetEndOfDay(new Date()),
    };

    const statusFilter = {
      type: "select",
      name: "status",
      options: [
        {
          label: t("common.status.all"),
          value: "all",
        },
        {
          label: t("positions.history.status.closed"),
          value: PositionHistoryStatus.closed,
        },
        {
          label: t("positions.history.status.partialClosed"),
          value: PositionHistoryStatus.partial_closed,
        },
      ],
      value: status,
    };

    if (isMobile) {
      return [sideFilter, statusFilter];
    }
    return [sideFilter, statusFilter, dateRangeFilter];
  }, [dateRange, isMobile, side, status, t]);

  return {
    filterItems,
    onFilter,
    side,
    dateRange,
    status,
    filterDays,
    updateFilterDays,
  };
};

export type PositionHistoryState = ReturnType<typeof usePositionHistoryScript>;
