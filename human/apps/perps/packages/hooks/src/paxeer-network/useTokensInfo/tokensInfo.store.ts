import { useMemo } from "react";
import { NetworkId } from "@paxeer-network/types";
import { useMainTokenStore } from "../../provider/store/mainTokenStore";
import { useTestTokenStore } from "../../provider/store/testTokenStore";
import { useAppStore } from "../appStore";

// interface TokensInfoStore {
//   tokensInfo: API.Chain[];
// }

// interface TokensInfoActions {
//   setTokensInfo: (data: API.Chain[]) => void;
// }

// export const useTokensInfoStore = create<TokensInfoStore & TokensInfoActions>(
//   (set) => ({
//     tokensInfo: [],
//     setTokensInfo(data) {
//       set({ tokensInfo: data });
//     },
//   }),
// );

/**
 * return all tokens info
 */
export const useTokensInfo = (networkId?: NetworkId) => {
  const appTokensInfo = useAppStore((state) => state.tokensInfo);
  const mainTokensInfo = useMainTokenStore((state) => state.data);
  const testTokensInfo = useTestTokenStore((state) => state.data);

  return useMemo(() => {
    if (networkId === "mainnet") {
      return mainTokensInfo;
    }

    if (networkId === "testnet") {
      return testTokensInfo;
    }

    return appTokensInfo;
  }, [appTokensInfo, mainTokensInfo, networkId, testTokensInfo]);
};

/**
 * return token info by specify token
 */
export const useTokenInfo = (token: string, networkId?: NetworkId) => {
  const tokensInfo = useTokensInfo(networkId);

  return useMemo(() => {
    return tokensInfo?.find((item) => item.token === token);
  }, [tokensInfo, token]);
};
