import React, { useMemo, useState } from "react";
import { getDate, getMonth, getYear, set } from "date-fns";
import {
  useAccount,
  useCollateral,
  useIndexPricesStream,
  useSubAccountQuery,
  useAppStore,
  useChainInfo,
} from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { EMPTY_LIST } from "@paxeer-network/types";
import { usePagination } from "@paxeer-network/ui";
import { subtractDaysFromCurrentDate } from "@paxeer-network/utils";
import { useAccountsData, useAssetsAccountFilter } from "../../../hooks";
import { parseDateRangeForFilter } from "../../overview/helper/date";
import type { ConvertRecord } from "../type";

export const useConvertScript = () => {
  const { t } = useTranslation();
  const { isMainAccount, state } = useAccount();
  const { holding = [] } = useCollateral();
  const { data: indexPrices } = useIndexPricesStream();

  const { data: chainsInfo } = useChainInfo();

  // Pagination
  const { page, pageSize, setPage, parsePagination } = usePagination({
    page: 1,
    pageSize: 20,
  });

  // Use the same account data structure as assets
  const allAccounts = useAccountsData();

  const tokensInfo = useAppStore((state) => state.tokensInfo);

  // Use the same account filter logic as assets
  const { selectedAccount, onAccountFilter } =
    useAssetsAccountFilter(allAccounts);

  // Simple date range handling like other scripts
  const [today] = useState(() => {
    const d = new Date();
    return new Date(getYear(d), getMonth(d), getDate(d), 0, 0, 0);
  });

  const [dateRange, setDateRange] = useState<Date[]>([
    subtractDaysFromCurrentDate(90, today),
    today,
  ]);

  // Filter states (matching API fields)
  const [statusFilter, setStatusFilter] = useState<string>("all");
  const [convertedAssetFilter, setConvertedAssetFilter] =
    useState<string>("all");

  // Create asset options from holding data - similar to assets script
  const convertedAssetOptions = useMemo(() => {
    // Create options array
    const assetOptions =
      tokensInfo?.map((item) => ({
        value: item.token,
        label: item.token,
      })) || [];

    return [{ label: t("common.allAssets"), value: "all" }, ...assetOptions];
  }, [tokensInfo, t]);

  // Handle all filters (including account filter and date range)
  const onFilter = React.useCallback(
    (filter: { name: string; value: string | { from: Date; to: Date } }) => {
      const { name, value } = filter;

      // Delegate account filter to the hook
      if (name === "account") {
        onAccountFilter(filter as { name: string; value: string });
      } else if (name === "status") {
        setStatusFilter(value as string);
        setPage(1); // Reset to first page when filter changes
      } else if (name === "converted_asset") {
        setConvertedAssetFilter(value as string);
        setPage(1);
      } else if (name === "time" || name === "dateRange") {
        setDateRange(
          parseDateRangeForFilter(value as { from: Date; to: Date }),
        );
        setPage(1);
      }
    },
    [onAccountFilter, setPage],
  );

  // Build query URL with parameters
  const queryUrl = React.useMemo(() => {
    const params = new URLSearchParams();

    // Add pagination parameters
    params.set("page", page.toString());
    params.set("size", pageSize.toString());

    // Add date range parameters
    if (dateRange[0]) {
      params.set("start_t", dateRange[0].getTime().toString());
    }
    if (dateRange[1]) {
      params.set(
        "end_t",
        set(dateRange[1], {
          hours: 23,
          minutes: 59,
          seconds: 59,
          milliseconds: 0,
        })
          .getTime()
          .toString(),
      );
    }

    // Add filter parameters (only if not "all")
    if (statusFilter !== "all") {
      params.set("status", statusFilter);
    }

    if (convertedAssetFilter !== "all") {
      params.set("converted_asset", convertedAssetFilter);
    }

    return `/v1/asset/convert_history?${params.toString()}`;
  }, [page, pageSize, dateRange, statusFilter, convertedAssetFilter]);

  // Query convert history with all parameters
  const { data, isLoading } = useSubAccountQuery<{
    rows: ConvertRecord[];
    meta: {
      total: number;
      current_page: number;
      records_per_page: number;
    };
  }>(queryUrl, {
    accountId: isMainAccount ? state.mainAccountId : state.accountId,
    formatter: (data) => {
      return data;
    },
  });

  // Parse pagination from API response meta
  const pagination = useMemo(() => {
    if (data?.meta) {
      return parsePagination(data.meta);
    }
    return {
      page,
      pageSize,
      onPageChange: setPage,
      onPageSizeChange: () => {
        // Handle page size change if needed
      },
    };
  }, [data?.meta, parsePagination, page, pageSize, setPage]);

  // Calculate summary
  const summary = useMemo(() => {
    const dataRows = data?.rows || [];

    const totalConversions = dataRows.length;
    const totalUSDCReceived = dataRows.reduce(
      (sum, record) => sum + record.received_qty,
      0,
    );
    const totalFees = dataRows.reduce(
      (sum, record) =>
        sum +
        record.details.reduce(
          (detailSum, detail) => detailSum + detail.haircut,
          0,
        ),
      0,
    );

    return {
      totalConversions,
      totalUSDCReceived,
      totalFees,
    };
  }, [data]);

  return {
    // Data
    dataSource: data?.rows || EMPTY_LIST,
    summary,
    pagination,

    // Filter state and handlers (compatible with assets)
    selectedAccount,
    onFilter,

    // Query parameters (consistent with other scripts)
    dateRange,

    // Filter states
    statusFilter,
    convertedAssetFilter,
    convertedAssetOptions,

    // State
    isMainAccount,
    isLoading,
    holding,

    // Index prices
    indexPrices,
    chainsInfo,
  };
};

export type UseConvertScriptReturn = ReturnType<typeof useConvertScript>;
