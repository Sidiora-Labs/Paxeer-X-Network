import {
  ADI_TESTNET_CHAINID,
  ABSTRACT_TESTNET_CHAINID,
  ARBITRUM_TESTNET_CHAINID,
  BSC_TESTNET_CHAINID,
  MONAD_TESTNET_CHAINID,
  SOLANA_TESTNET_CHAINID,
  STORY_TESTNET_CHAINID,
  SOLANA_MAINNET_CHAINID,
  XLAYER_TESTNET_CHAINID,
} from "@paxeer-network/types";

export const hex2int = (chainId: string): number => parseInt(chainId);
export const int2hex = (chainId: number): string => `0x${chainId.toString(16)}`;
export const praseChainId = (chainId: string | number): number => {
  if (typeof chainId === "string") return hex2int(chainId);
  return chainId;
};

export const praseChainIdToNumber = (chainId: string | number): number => {
  if (
    typeof chainId === "string" &&
    chainId.startsWith("0x") &&
    /^[a-f0-9]+$/iu.test(chainId.slice(2))
  )
    return hex2int(chainId);
  return chainId as number;
};

export const parseChainIdToNumber = praseChainIdToNumber;

/**
 * @deprecated This helper relies on a static chain ID list. Use
 * `useChains().isTestnetChain` to detect testnet chains from API data.
 */
export const isTestnet = (chainId: number) => {
  const testnetIds = [
    ARBITRUM_TESTNET_CHAINID,
    SOLANA_TESTNET_CHAINID,
    STORY_TESTNET_CHAINID,
    MONAD_TESTNET_CHAINID,
    ABSTRACT_TESTNET_CHAINID,
    BSC_TESTNET_CHAINID,
    ADI_TESTNET_CHAINID,
    XLAYER_TESTNET_CHAINID,
  ];

  return testnetIds.includes(chainId);
};

export const isSolana = (chainId: number) => {
  return [SOLANA_TESTNET_CHAINID, SOLANA_MAINNET_CHAINID].includes(chainId);
};
