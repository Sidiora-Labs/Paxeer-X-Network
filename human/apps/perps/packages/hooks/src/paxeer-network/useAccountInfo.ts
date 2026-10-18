import type { API } from "@paxeer-network/types";
import { usePrivateQuery } from "../usePrivateQuery";

export const useAccountInfo = () => {
  return usePrivateQuery<API.AccountInfo>("/v1/client/info", {
    revalidateOnFocus: false,
  });
};
