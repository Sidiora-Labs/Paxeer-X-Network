import { useQuery } from "../useQuery";

export const useChainInfo = () => {
  return useQuery("/v1/public/chain_info", {
    revalidateOnFocus: false,
  });
};
