import { useAccount } from "@paxeer-network/hooks";
import { AccountStatusEnum } from "@paxeer-network/types";
import { useAppContext } from "../provider/appStateContext";

export const useDataTap = <T = any>(
  data: T,
  options?: {
    skip?: false;
    fallbackData?: T;
    accountStatus?: AccountStatusEnum;
  },
): T | null => {
  const { wrongNetwork, disabledConnect } = useAppContext();
  const { state } = useAccount();
  /**
   * ignore
   */
  if (options?.skip) {
    return data;
  }

  if (wrongNetwork || disabledConnect) {
    return typeof options?.fallbackData !== "undefined"
      ? options.fallbackData
      : null;
  }

  if (typeof options?.accountStatus !== "undefined") {
    if (state.status < options.accountStatus) {
      return typeof options?.fallbackData !== "undefined"
        ? options.fallbackData
        : null;
    }
  }

  // return wrongNetwork
  //   ? typeof options?.fallbackData !== "undefined"
  //     ? options.fallbackData
  //     : null
  //   : data;
  //
  return data;
};
