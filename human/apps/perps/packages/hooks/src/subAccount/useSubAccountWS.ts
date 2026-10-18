import { useContext, useEffect, useRef } from "react";
import { WS } from "@paxeer-network/net";
import { AccountStatusEnum } from "@paxeer-network/types";
import { getTimestamp } from "@paxeer-network/utils";
import { PaxeerNetworkContext } from "../paxeerNetworkContext";
import { useAccount } from "../useAccount";

type Options = {
  accountId?: string;
};

export const useSubAccountWS = (options: Options) => {
  const { accountId } = options;
  const { configStore } = useContext(PaxeerNetworkContext);
  const { state, account } = useAccount();

  const websocketClient = useRef<WS>(
    new WS({
      networkId: configStore.get("networkId"),
      // not need to subscribe public socket
      // publicUrl: configStore.get("publicWsUrl"),
      privateUrl: configStore.get("privateWsUrl"),
      onSigntureRequest: async () => {
        const signer = account.signer;
        const timestamp = getTimestamp();
        const result = await signer.signText(timestamp.toString());
        return { ...result, timestamp };
      },
    }),
  );

  useEffect(() => {
    // if accountId is exist and user login, open the private websocket
    if (
      accountId &&
      (state.status === AccountStatusEnum.EnableTrading ||
        state.status === AccountStatusEnum.EnableTradingWithoutConnected)
    ) {
      websocketClient.current.openPrivate(accountId);
    } else {
      // when accountId is not exist and user logout, close the private websocket
      // websocketClient.current.closePrivate(3887, "switch account");
    }

    return () => {
      // when unmount, close the private websocket
      websocketClient.current.closePrivate(3887, "switch account");
    };
  }, [accountId, state.status]);

  return websocketClient.current;
};
