/* eslint-disable @typescript-eslint/no-explicit-any */
import { useMemo } from "react";
import { produce } from "immer";
import { useAccount, useCollateral } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import type { API } from "@paxeer-network/types";

const EMPTY_HOLDING: Partial<API.Holding> = {
  token: "USDC",
  holding: 0,
  frozen: 0,
};

// Account data structure used in assets display
export interface AccountWithChildren {
  account_id: string;
  id?: string;
  description?: string;
  children: Array<API.Holding & { account_id: string }>;
}

// Hook to transform raw account data into display format
export const useAccountsData = (): AccountWithChildren[] => {
  const { t } = useTranslation();
  const { state, isMainAccount } = useAccount();
  const { holding = [] } = useCollateral();
  const subAccounts = state.subAccounts ?? [];

  return useMemo(() => {
    return produce<any[]>(subAccounts, (draft) => {
      // Process sub accounts
      for (const sub of draft) {
        sub.account_id = sub.id;
        if (Array.isArray(sub.holding) && sub.holding.length) {
          sub.children = sub.holding.map((item: API.Holding) => ({
            ...item,
            account_id: sub.id,
          }));
        } else {
          sub.children = [{ ...EMPTY_HOLDING, account_id: sub.id }];
        }
        // Remove original holding to avoid confusion
        Reflect.deleteProperty(sub, "holding");
      }

      // Add main account at the beginning if user is main account
      if (isMainAccount) {
        draft.unshift({
          account_id: state.mainAccountId!,
          description: t("common.mainAccount"),
          children:
            Array.isArray(holding) && holding.length
              ? holding.map((item: API.Holding) => ({
                  ...item,
                  account_id: state.mainAccountId!,
                }))
              : [
                  {
                    ...EMPTY_HOLDING,
                    account_id: state.mainAccountId!,
                  },
                ],
        });
      }
    });
  }, [holding, subAccounts, isMainAccount, state.mainAccountId, t]);
};
