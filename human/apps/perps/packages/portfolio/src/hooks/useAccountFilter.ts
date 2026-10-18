import { useMemo } from "react";
import { useAccount } from "@paxeer-network/hooks";
import { AccountType } from "../pages/assets/assetsPage/assets.ui.desktop";

// Account filter function type
export type AccountFilterFunction<T = any> = (
  data: T[],
  selectedAccount: string,
  accountState: ReturnType<typeof useAccount>,
) => T[];

// Generic account filter utility
export const createAccountFilter = <T extends { account_id?: string }>(
  getAccountId?: (item: T) => string | undefined,
): AccountFilterFunction<T> => {
  return (data, selectedAccount, accountState) => {
    const { state, isMainAccount } = accountState;

    return data.filter((item) => {
      const accountId = getAccountId ? getAccountId(item) : item.account_id;

      if (isMainAccount) {
        if (!selectedAccount || selectedAccount === AccountType.ALL) {
          return true;
        }
        if (selectedAccount === AccountType.MAIN) {
          return accountId === state.mainAccountId;
        } else {
          return accountId === selectedAccount;
        }
      } else {
        return accountId === state.accountId;
      }
    });
  };
};

// Hook for standard account-based data filtering
export const useAccountFilter = <T extends { account_id?: string }>(
  data: T[],
  selectedAccount: string,
  getAccountId?: (item: T) => string | undefined,
): T[] => {
  const accountState = useAccount();

  return useMemo(() => {
    const { state, isMainAccount } = accountState;

    return data.filter((item) => {
      const accountId = getAccountId ? getAccountId(item) : item.account_id;

      if (isMainAccount) {
        if (!selectedAccount || selectedAccount === AccountType.ALL) {
          return true;
        }
        if (selectedAccount === AccountType.MAIN) {
          return accountId === state.mainAccountId;
        } else {
          return accountId === selectedAccount;
        }
      } else {
        return accountId === state.accountId;
      }
    });
  }, [data, selectedAccount, accountState, getAccountId]);
};

// Hook specifically for transfer history filtering
export const useTransferHistoryAccountFilter = <
  T extends { from_account_id?: string; to_account_id?: string },
>(
  data: T[],
  selectedAccount: string,
): T[] => {
  const accountState = useAccount();

  return useMemo(() => {
    const { state, isMainAccount } = accountState;

    return data.filter((item) => {
      if (isMainAccount) {
        if (!selectedAccount || selectedAccount === AccountType.ALL) {
          return true;
        }
        if (selectedAccount === AccountType.MAIN) {
          return (
            item.from_account_id === state.mainAccountId ||
            item.to_account_id === state.mainAccountId
          );
        } else {
          return (
            item.from_account_id === selectedAccount ||
            item.to_account_id === selectedAccount
          );
        }
      } else {
        return (
          item.from_account_id === state.accountId ||
          item.to_account_id === state.accountId
        );
      }
    });
  }, [data, selectedAccount, accountState]);
};

// Pre-defined account filters for common use cases
export const AccountFilters = {
  // Standard account filter factory
  standard: <T extends { account_id?: string }>() => createAccountFilter<T>(),

  // Transfer history filter factory
  transferHistory:
    <T extends { from_account_id?: string; to_account_id?: string }>() =>
    (
      data: T[],
      selectedAccount: string,
      accountState: ReturnType<typeof useAccount>,
    ): T[] => {
      const { state, isMainAccount } = accountState;

      return data.filter((item) => {
        if (isMainAccount) {
          if (!selectedAccount || selectedAccount === AccountType.ALL) {
            return true;
          }
          if (selectedAccount === AccountType.MAIN) {
            return (
              item.from_account_id === state.mainAccountId ||
              item.to_account_id === state.mainAccountId
            );
          } else {
            return (
              item.from_account_id === selectedAccount ||
              item.to_account_id === selectedAccount
            );
          }
        } else {
          return (
            item.from_account_id === state.accountId ||
            item.to_account_id === state.accountId
          );
        }
      });
    },

  // Custom account ID filter factory
  customAccountId: <T extends Record<string, any>>(accountIdKey: keyof T) =>
    createAccountFilter<T>((item) => item[accountIdKey] as string),
};
