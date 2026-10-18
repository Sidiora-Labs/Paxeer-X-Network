import { useMemo } from "react";
import { useAccount } from "@paxeer-network/hooks";
import { useAppContext } from "@paxeer-network/react-app";
import { AccountStatusEnum } from "@paxeer-network/types";

export enum AuthStatusEnum {
  WrongNetwork,
  ConnectWallet,
  CreateAccount,
  EnableTrading,
}

export const useAuthStatus = () => {
  const { state } = useAccount();
  const { wrongNetwork, disabledConnect } = useAppContext();

  return useMemo(() => {
    if (wrongNetwork && !disabledConnect) {
      return AuthStatusEnum.WrongNetwork;
    }

    if (state.status === AccountStatusEnum.EnableTradingWithoutConnected) {
      return AuthStatusEnum.EnableTrading;
    }

    if (state.status <= AccountStatusEnum.NotConnected || disabledConnect) {
      return AuthStatusEnum.ConnectWallet;
    }

    if (state.status <= AccountStatusEnum.NotSignedIn) {
      return AuthStatusEnum.CreateAccount;
    }

    return AuthStatusEnum.EnableTrading;
  }, [state.status, wrongNetwork, disabledConnect]);
};
