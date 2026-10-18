import React, { useMemo } from "react";
import { useAccount } from "@paxeer-network/hooks";
import { AccountType } from "../pages/assets/assetsPage/assets.ui.desktop";

// Account filter hook for assets-like data structures
export const useAssetsAccountFilter = <T extends { account_id?: string }>(
  data: T[],
) => {
  const { state, isMainAccount } = useAccount();
  const [selectedAccount, setSelectedAccount] = React.useState<string>(
    AccountType.ALL,
  );

  // Filter data based on selected account
  const filteredData = useMemo(() => {
    return data.filter((item) => {
      if (isMainAccount) {
        if (!selectedAccount || selectedAccount === AccountType.ALL) {
          return true;
        }
        if (selectedAccount === AccountType.MAIN) {
          return item.account_id === state.mainAccountId;
        } else {
          return item.account_id === selectedAccount;
        }
      } else {
        return item.account_id === state.accountId;
      }
    });
  }, [data, selectedAccount, isMainAccount, state]);

  // Handle filter changes
  const onAccountFilter = React.useCallback(
    (filter: { name: string; value: string }) => {
      const { name, value } = filter;
      if (name === "account") {
        setSelectedAccount(value);
      }
    },
    [],
  );

  return {
    selectedAccount,
    filteredData,
    onAccountFilter,
    setSelectedAccount,
  };
};

// Enhanced account and asset combined filter hook
export const useAssetsMultiFilter = <
  T extends {
    account_id?: string;
    children?: Array<{ token: string; [key: string]: any }>;
  },
>(
  data: T[],
) => {
  const { state, isMainAccount } = useAccount();
  const [selectedAccount, setSelectedAccount] = React.useState<string>(
    AccountType.ALL,
  );
  const [selectedAsset, setSelectedAsset] = React.useState<string>("all");

  // Filter data based on both account and asset
  const filteredData = useMemo(() => {
    let accountFiltered = data;

    // First apply account filter
    if (isMainAccount) {
      if (selectedAccount && selectedAccount !== AccountType.ALL) {
        if (selectedAccount === AccountType.MAIN) {
          accountFiltered = data.filter(
            (item) => item.account_id === state.mainAccountId,
          );
        } else {
          accountFiltered = data.filter(
            (item) => item.account_id === selectedAccount,
          );
        }
      }
    } else {
      accountFiltered = data.filter(
        (item) => item.account_id === state.accountId,
      );
    }

    // Then apply asset filter
    if (selectedAsset && selectedAsset !== "all") {
      accountFiltered = accountFiltered
        .map((account) => {
          if (account.children) {
            const filteredChildren = account.children.filter(
              (child) => child.token === selectedAsset,
            );
            // Only include accounts that have the selected asset
            return filteredChildren.length > 0
              ? { ...account, children: filteredChildren }
              : null;
          }
          return account;
        })
        .filter(Boolean) as T[];
    }

    return accountFiltered;
  }, [data, selectedAccount, selectedAsset, isMainAccount, state]);

  // Handle combined filter changes
  const onFilter = React.useCallback(
    (filter: { name: string; value: string }) => {
      const { name, value } = filter;
      if (name === "account") {
        setSelectedAccount(value);
      } else if (name === "asset") {
        setSelectedAsset(value);
      }
    },
    [],
  );

  return {
    selectedAccount,
    selectedAsset,
    filteredData,
    onFilter,
    setSelectedAccount,
    setSelectedAsset,
  };
};

// Generic account filter function that can be used standalone
export const filterByAccount = <T extends { account_id?: string }>(
  data: T[],
  selectedAccount: string,
  accountState: {
    isMainAccount: boolean;
    state: {
      mainAccountId?: string;
      accountId?: string;
    };
  },
): T[] => {
  const { isMainAccount, state } = accountState;

  return data.filter((item) => {
    if (isMainAccount) {
      if (!selectedAccount || selectedAccount === AccountType.ALL) {
        return true;
      }
      if (selectedAccount === AccountType.MAIN) {
        return item.account_id === state.mainAccountId;
      } else {
        return item.account_id === selectedAccount;
      }
    } else {
      return item.account_id === state.accountId;
    }
  });
};
