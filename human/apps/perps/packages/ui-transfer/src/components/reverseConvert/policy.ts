import {
  ROBINHOOD_MAINNET_CHAINID,
  ROBINHOOD_TESTNET_CHAINID,
} from "@paxeer-network/types";

const ROBINHOOD_CHAIN_IDS = new Set([
  ROBINHOOD_MAINNET_CHAINID,
  ROBINHOOD_TESTNET_CHAINID,
]);

export const isRobinhoodChain = (chainId: unknown) =>
  ROBINHOOD_CHAIN_IDS.has(Number(chainId));
/** Fixed 0.5% request slippage for reverse quote and submit. CeFi still requires it. */
export const REVERSE_CONVERT_SLIPPAGE = 0.005;
export const canReverseConvert = (
  chainId: unknown,
  from?: string,
  to?: string,
) => isRobinhoodChain(chainId) && from === "USDC" && to === "USDG";
