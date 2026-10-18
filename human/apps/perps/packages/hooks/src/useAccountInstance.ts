import useConstant from "use-constant";
import { Account, SimpleDI } from "@paxeer-network/core";
import { useContext } from "react";
import { PaxeerNetworkContext } from "./paxeerNetworkContext";
import { SDKError } from "@paxeer-network/types";

export const useAccountInstance = (): Account => {
  const { configStore, keyStore, walletAdapters } = useContext(PaxeerNetworkContext);

  if (!configStore)
    throw new SDKError(
      "configStore is not defined, please use PaxeerNetworkProvider"
    );

  if (!keyStore) {
    throw new SDKError(
      "keyStore is not defined, please use PaxeerNetworkProvider and provide keyStore"
    );
  }

  const account = useConstant(() => {
    let account = SimpleDI.get<Account>("account");

    if (!account) {
      account = new Account(
        configStore,
        keyStore,
        // getWalletAdapter,
        walletAdapters
      );

      SimpleDI.registerByName("account", account);
    }
    return account;
  });

  return account;
};
