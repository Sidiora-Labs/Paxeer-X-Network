import { useMemo } from "react";
import { useAccount } from "@paxeer-network/hooks";
import { useAppContext } from "@paxeer-network/react-app";
import { AccountStatusEnum } from "@paxeer-network/types";

/**
 * Hook to check if the user is authenticated and authorized
 * @param status - Required account status to be satisfied. If not provided, defaults to EnableTrading or EnableTradingWithoutConnected based on current state
 * @returns boolean indicating if the user meets the authentication requirements
 */
export const useAuthGuard = (status?: AccountStatusEnum): boolean => {
  const { state } = useAccount();
  const { wrongNetwork, disabledConnect } = useAppContext();

  const _status = useMemo(() => {
    if (status === undefined) {
      return state.status === AccountStatusEnum.EnableTradingWithoutConnected
        ? AccountStatusEnum.EnableTradingWithoutConnected
        : AccountStatusEnum.EnableTrading;
    }
    return status;
  }, [status, state.status]);

  return useMemo(() => {
    return state.status >= _status && !wrongNetwork && !disabledConnect;
  }, [state.status, _status, wrongNetwork, disabledConnect]);
};
