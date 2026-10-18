import { useCallback, useContext, useEffect, useMemo, useState } from "react";
import { AccountState } from "@paxeer-network/core";
import {
  AccountStatusEnum,
  SDKError,
  TrackerEventName,
} from "@paxeer-network/types";
import { PaxeerNetworkContext } from "./paxeerNetworkContext";
import { useAccountInstance } from "./useAccountInstance";
import { useEventEmitter } from "./useEventEmitter";
import { useTrack } from "./useTrack";
import { useWS } from "./useWS";

export const useAccount = () => {
  const { configStore, keyStore } = useContext(PaxeerNetworkContext);

  if (!configStore)
    throw new SDKError(
      "configStore is not defined, please use PaxeerNetworkProvider",
    );

  if (!keyStore) {
    throw new SDKError(
      "keyStore is not defined, please use PaxeerNetworkProvider and provide keyStore",
    );
  }

  const account = useAccountInstance();

  // const [subAccounts, setSubAccounts] = useState<SubAccount[]>([]);

  const [state, setState] = useState<AccountState>(account.stateValue);
  const { track } = useTrack();

  const statusChangeHandler = (nextState: AccountState) => {
    setState(() => nextState);
  };

  useEffect(() => {
    account.on("change:status", statusChangeHandler);

    return () => {
      account.off("change:status", statusChangeHandler);
    };
  }, [account]);

  const createPaxeerNetworkKey = useCallback(
    async (remember: boolean) => {
      track(TrackerEventName.signinSuccess, {
        network: account.chainId,
        wallet: state.connectWallet?.name,
      });
      return account.createPaxeerNetworkKey(remember ? 365 : 30).then((res) => {
        return account.restoreSubAccount().then((_) => {
          return res;
        });
      });
    },
    [account, state],
  );

  const ws = useWS();

  const switchAccount = useCallback(
    async (accountId: string) => {
      // close existing private connection when switch account
      ws.closePrivate(3887, "switch account");
      return account.switchAccount(accountId);
    },
    [account],
  );

  const createAccount = useCallback(async () => {
    return account.createAccount();
  }, [account]);

  const createSubAccount = useCallback(
    async (description?: string) => {
      return account.createSubAccount(description);
    },
    [account],
  );

  const updateSubAccount = useCallback(
    async (value: { subAccountId: string; description?: string }) => {
      return account.updateSubAccount(value);
    },
    [account],
  );

  const refreshSubAccountBalances = useCallback(() => {
    return account.refreshSubAccountBalances();
  }, [account]);

  const isSubAccount = useMemo(() => {
    // Not connected / no active account → not a sub-account
    if (!state.accountId || !state.mainAccountId) {
      return false;
    }
    return state.accountId !== state.mainAccountId;
  }, [state]);

  return {
    account,
    state,
    isSubAccount,
    isMainAccount: !isSubAccount,
    subAccount: {
      refresh: refreshSubAccountBalances,
      create: createSubAccount,
      update: updateSubAccount,
    },
    switchAccount,
    createPaxeerNetworkKey,
    createAccount,
  };
};
