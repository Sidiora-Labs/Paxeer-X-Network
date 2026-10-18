import { useCallback, useEffect, useMemo } from "react";
import {
  SubAccount,
  useAccount,
  usePositionStream,
  usePrivateQuery,
  useSymbolsInfo,
} from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { useDataTap } from "@paxeer-network/react-app";
import { type API } from "@paxeer-network/types";
import { formatAddress, usePagination } from "@paxeer-network/ui";
import type { PositionsProps } from "../../types/types";
import { calculatePositions } from "./calculatePositions";
import { useSubAccountQuery } from "./hooks/useSubAccountQuery";
import { useSubAccountTPSL } from "./hooks/useSubAccountTPSL";

export enum AccountType {
  ALL = "All accounts",
  MAIN = "Main accounts",
}

export const useCombinePositionsScript = (props: PositionsProps) => {
  const {
    symbol,
    calcMode,
    includedPendingOrder,
    pnlNotionalDecimalPrecision,
    sharePnLConfig,
    onSymbolChange,
    selectedAccount,
  } = props;

  const { pagination, setPage } = usePagination({ pageSize: 50 });

  const symbolsInfo = useSymbolsInfo();

  const { state } = useAccount();
  const { t } = useTranslation();

  const [mainAccountPositions, , { isLoading }] = usePositionStream(symbol, {
    calcMode,
    includedPendingOrder,
  });

  const {
    data: newPositions = [],
    isLoading: isPositionLoading,
    mutate: mutatePositions,
  } = usePrivateQuery<API.PositionExt[]>("/v1/client/aggregate/positions", {
    errorRetryCount: 3,
  });

  // Get unique sub-account IDs
  const { allAccountIds, subAccountIds } = useMemo(() => {
    const uniqueIds = new Set(
      newPositions
        .filter((item) => item.account_id)
        .map((item) => item.account_id!)
        .filter(Boolean),
    );
    const allAccountIds = Array.from(uniqueIds);
    const subAccountIds = allAccountIds.filter(
      (item) => item !== state.mainAccountId,
    );

    return { allAccountIds, subAccountIds };
  }, [newPositions, state.mainAccountId]);

  // need to get sub account info to calculate portfolio and positions
  const { data: accountInfo = [], isLoading: isAccountInfoLoading } =
    useSubAccountQuery<API.AccountInfo[]>("/v1/client/info", {
      accountId: allAccountIds,
      revalidateOnFocus: false,
    });

  const { tpslOrders, mutateTPSLOrders } = useSubAccountTPSL(subAccountIds);

  const subAccountPositions = useMemo(() => {
    return calculatePositions(
      newPositions.filter((item) => item.account_id !== state.mainAccountId),
      symbolsInfo,
      accountInfo,
      tpslOrders,
    );
  }, [newPositions, symbolsInfo, accountInfo, state.mainAccountId, tpslOrders]);

  const allPositions = useMemo(() => {
    return [...mainAccountPositions?.rows, ...subAccountPositions].filter(
      (item) => item.position_qty !== 0,
    );
  }, [mainAccountPositions, subAccountPositions]);

  const dataSource = useDataTap(allPositions) ?? [];

  const filtered = useMemo(() => {
    if (!selectedAccount || selectedAccount === AccountType.ALL) {
      return dataSource;
    }
    return dataSource.filter((item) => {
      if (selectedAccount === AccountType.MAIN) {
        return item.account_id === state.mainAccountId || !item.account_id;
      } else {
        return item.account_id === selectedAccount;
      }
    });
  }, [dataSource, selectedAccount, state.mainAccountId]);

  const groupDataSource = useMemo(() => {
    return groupDataByAccount(filtered, {
      mainAccountId: state.mainAccountId,
      mainAccountLabel: t("common.mainAccount"),
      subAccounts: state.subAccounts,
    });
  }, [filtered, state.mainAccountId, state.subAccounts, t]);

  const loading = isLoading || isPositionLoading || isAccountInfoLoading;

  useEffect(() => {
    setPage(1);
  }, [symbol]);

  const mutateList = useCallback(() => {
    mutatePositions();
    mutateTPSLOrders();
  }, []);

  return {
    tableData: groupDataSource,
    isLoading: loading,
    pnlNotionalDecimalPrecision,
    sharePnLConfig,
    symbol,
    onSymbolChange,
    pagination,
    mutatePositions: mutateList,
  };
};

export type CombinePositionsState = ReturnType<
  typeof useCombinePositionsScript
>;

const groupDataByAccount = (
  data: API.PositionExt[],
  options: {
    mainAccountId?: string;
    mainAccountLabel: string;
    subAccounts?: SubAccount[];
  },
) => {
  const { mainAccountId = "", mainAccountLabel, subAccounts = [] } = options;

  const map = new Map<
    PropertyKey,
    {
      account_id: string;
      description: string;
      children: API.PositionExt[];
    }
  >();

  for (const item of data) {
    // if account_id is not set, as a main account
    const accountId = item.account_id || mainAccountId;
    const findSubAccount = subAccounts.find((item) => item.id === accountId);
    if (map.has(accountId)) {
      map.get(accountId)?.children?.push(item);
    } else {
      map.set(accountId, {
        account_id: accountId,
        description:
          accountId === mainAccountId
            ? mainAccountLabel
            : findSubAccount?.description ||
              formatAddress(findSubAccount?.id || ""),
        children: [item],
      });
    }
  }
  return {
    expanded: Array.from(map.keys()),
    dataSource: Array.from(map.values()),
  };
};
