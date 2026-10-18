import { useCallback, useMemo, useState } from "react";
import { getDate, getMonth, getYear, set } from "date-fns";
import {
  useAssetsHistory,
  useChainInfo,
  useAppStore,
  useTransferHistory,
} from "@paxeer-network/hooks";
import { AssetHistorySideEnum } from "@paxeer-network/types";
import { modal, usePagination } from "@paxeer-network/ui";
import { DepositAndWithdrawWithSheetId } from "@paxeer-network/ui-transfer";
import { subtractDaysFromCurrentDate } from "@paxeer-network/utils";
import { parseDateRangeForFilter } from "../helper/date";

export enum AssetTarget {
  Web3Wallet = "Web3Wallet",
  AccountId = "AccountId",
}

export type AssetHistoryScriptOptions = {
  side: AssetHistorySideEnum;
};

export type AssetHistoryScriptReturn = ReturnType<typeof useAssetHistoryScript>;

export const useAssetHistoryScript = (options: AssetHistoryScriptOptions) => {
  const { side } = options;
  const isDeposit = side === AssetHistorySideEnum.DEPOSIT;

  const [today] = useState(() => {
    const d = new Date();
    return new Date(getYear(d), getMonth(d), getDate(d), 0, 0, 0);
  });

  const tokensInfo = useAppStore((state) => state.tokensInfo);

  const [target, setTarget] = useState<AssetTarget>(AssetTarget.Web3Wallet);

  const [dateRange, setDateRange] = useState<Date[]>([
    subtractDaysFromCurrentDate(90, today),
    today,
  ]);
  const { page, pageSize, setPage, parsePagination } = usePagination();

  const { startTime, endTime } = useMemo(() => {
    const startTime = dateRange[0].getTime();
    const endTime = set(dateRange[1], {
      hours: 23,
      minutes: 59,
      seconds: 59,
      milliseconds: 0,
    }).getTime();

    return { startTime, endTime };
  }, [dateRange]);

  const [assetData, { meta: assetMeta, isLoading: assetLoading }] =
    useAssetsHistory(
      {
        startTime,
        endTime,
        page,
        pageSize,
        side,
      },
      {
        shouldUpdateOnWalletChanged: (data) => data.side === side,
      },
    );

  const [transferData, { isLoading: transferLoading, meta: transferMeta }] =
    useTransferHistory({
      dataRange: [startTime, endTime],
      side: isDeposit ? "IN" : "OUT",
      size: pageSize,
      page: page,
      main_sub_only: false,
    });

  const { data: chainsInfo } = useChainInfo();

  const onFilter = (filter: { name: string; value: any }) => {
    if (filter.name === "dateRange") {
      setDateRange(parseDateRangeForFilter(filter.value));
      setPage(1);
    } else if (filter.name === "target") {
      setTarget(filter.value);
      setPage(1);
    }
  };

  const isLoading = useMemo(() => {
    if (target === AssetTarget.Web3Wallet) {
      return assetLoading;
    }
    return transferLoading;
  }, [target, assetLoading, transferLoading]);

  const meta = useMemo(() => {
    if (target === AssetTarget.Web3Wallet) {
      return assetMeta;
    }
    return transferMeta;
  }, [target, assetMeta, transferMeta]);

  const pagination = useMemo(
    () => parsePagination(meta),
    [parsePagination, meta],
  );

  const dataSource = useMemo(() => {
    return (target === AssetTarget.Web3Wallet ? assetData : transferData).map(
      (item) => {
        const findToken = tokensInfo?.find(({ token }) => token === item.token);
        return {
          ...item,
          decimals: findToken?.decimals ?? 2,
        };
      },
    );
  }, [target, assetData, transferData, tokensInfo]);

  const onDeposit = useCallback(() => {
    modal.show(DepositAndWithdrawWithSheetId, { activeTab: "deposit" });
  }, []);

  const isWeb3Wallet = target === AssetTarget.Web3Wallet;

  return {
    dataSource,
    total: meta?.total,
    isLoading,
    queryParameter: {
      target,
      dateRange,
    },
    onFilter,
    pagination,
    onDeposit,
    chainsInfo: chainsInfo as any[],
    isDeposit,
    isWeb3Wallet,
  };
};
