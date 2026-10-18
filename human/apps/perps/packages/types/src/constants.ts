/* eslint-disable @typescript-eslint/no-explicit-any */

export enum AccountStatusEnum {
  EnableTradingWithoutConnected = -1,
  NotConnected = 0,
  Connected = 1,
  NotSignedIn = 2,
  SignedIn = 3,
  DisabledTrading = 4,
  EnableTrading = 5,
}

export enum SystemStateEnum {
  Loading = 0,
  Error = 1,
  Ready = 10,
}

export enum ExchangeStatusEnum {
  Normal = 0,
  Maintain = 1,
}

export type NetworkId = "testnet" | "mainnet";

export enum NetworkStatusEnum {
  unknown = 0,
  unsupported = 1,
  supported = 2,
}

// Testnet
// Arbitrum Goerli
// export const ARBITRUM_TESTNET_CHAINID = 421613;
// export const ARBITRUM_TESTNET_CHAINID_HEX = "0x66EED";

// Arbitrum Sepolia
export const ARBITRUM_TESTNET_CHAINID = 421614;
export const SOLANA_TESTNET_CHAINID = 901901901;
export const SOLANA_MAINNET_CHAINID = 900900900;
export const STORY_TESTNET_CHAINID = 1516;
export const MONAD_TESTNET_CHAINID = 10143;
export const ABSTRACT_TESTNET_CHAINID = 11124;
export const ABSTRACT_MAINNET_CHAINID = 2741;
export const BSC_TESTNET_CHAINID = 97;
export const ADI_TESTNET_CHAINID = 99999;
export const XLAYER_TESTNET_CHAINID = 1952;
export const ABSTRACT_CHAIN_ID_MAP = new Set([
  ABSTRACT_TESTNET_CHAINID,
  ABSTRACT_MAINNET_CHAINID,
]);

export const ROBINHOOD_MAINNET_CHAINID = 4663;
export const ROBINHOOD_TESTNET_CHAINID = 46630;

export const ARBITRUM_TESTNET_CHAINID_HEX = "0x66EEE";

export const MANTLE_TESTNET_CHAINID = 5003;
export const MANTLE_TESTNET_CHAINID_HEX = "0x138b";

// Mainnet
export const ARBITRUM_MAINNET_CHAINID = 42161;
export const ETHEREUM_MAINNET_CHAINID = 1;
export const ADI_MAINNET_CHAINID = 36900;
export const ARBITRUM_MAINNET_CHAINID_HEX = "0xa4b1";

/** @deprecated use useScreen hook instead */
export const MEDIA_TABLET = "(max-width: 1023.98px)";

/** deposit buffer factor */
export const DEPOSIT_FEE_RATE = 1.05;

/**
 *  A constant for the maximum value for a ``uint256``.
 */
export const MaxUint256: bigint = BigInt(
  "0xffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
);

export const nativeTokenAddress = "0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE";

// TODO: remove this
export const nativeETHAddress = "0x0000000000000000000000000000000000000000";

export const isNativeTokenChecker = (address?: string) => {
  if (!address) {
    return false;
  }
  return (
    address?.toLowerCase() === nativeTokenAddress.toLowerCase() ||
    address === nativeETHAddress
  );
};

export const ArbitrumMainnetChainInfo = {
  name: "Arbitrum",
  public_rpc_url: "https://arb1.arbitrum.io/rpc",
  chain_id: "42161",
  currency_symbol: "ETH",
  currency_decimal: 18,
  explorer_base_url: "https://arbiscan.io",
  vault_address: "0x816f722424B49Cf1275cc86DA9840Fbd5a6167e9",
};
export const ArbitrumSepoliaChainInfo = {
  name: "Arbitrum Sepolia",
  public_rpc_url: "https://arbitrum-sepolia.gateway.tenderly.co",
  chain_id: "421614",
  currency_symbol: "ETH",
  currency_decimal: 18,
  explorer_base_url: "https://sepolia.arbiscan.io",
  vault_address: "0x0EaC556c0C2321BA25b9DC01e4e3c95aD5CDCd2f",
};
export const AbstractTestnetChainInfo = {
  name: "Abstract Testnet",
  public_rpc_url: "https://api.testnet.abs.xyz",
  chain_id: "11124",
  currency_symbol: "ETH",
  currency_decimal: 18,
  explorer_base_url: "https://sepolia.abscan.org",
  vault_address: "0xf14Ff11F3bb1011ff42665Ec869c7827c43745Fd",
};
export const AbstractTestnetTokenInfo = {
  chain_id: "11124",
  contract_address: "0xa0BB43E2eA7fcE91F07e628d72fD6333e80F47D2",
  decimals: 6,
  withdrawal_fee: 1,
  cross_chain_withdrawal_fee: 2,
  display_name: "USDC",
};
export const SolanaMainnetChainInfo = {
  chain_id: "900900900",
  currency_decimal: 9,
  currency_symbol: "SOL",
  explorer_base_url: "https://explorer.solana.com",
  name: "Solana",
  public_rpc_url: "https://api.mainnet-beta.solana.com",
  vault_address: "ErBmAD61mGFKvrFNaTJuxoPwqrS8GgtwtqJTJVjFWx9Q",
};
export const SolanaDevnetChainInfo = {
  chain_id: "901901901",
  currency_decimal: 9,
  currency_symbol: "SOL",
  explorer_base_url: "https://explorer.solana.com/?cluster=devnet",
  name: "Solana-Devnet",
  public_rpc_url: "https://api.devnet.solana.com",
  vault_address: "9shwxWDUNhtwkHocsUAmrNAQfBH2DHh4njdAEdHZZkF2",
};

export const SolanaDevnetTokenInfo = {
  chain_id: "901901901",
  contract_address: "4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU",
  decimals: 6,
  withdrawal_fee: 1,
  cross_chain_withdrawal_fee: 5,
  display_name: "USDC",
};
export const ArbitrumSepoliaTokenInfo = {
  chain_id: "421614",
  contract_address: "0x75faf114eafb1BDbe2F0316DF893fd58CE46AA4d",
  decimals: 6,
  withdrawal_fee: 1,
  cross_chain_withdrawal_fee: 2,
  display_name: "USDC",
};

export const TesnetTokenFallback = (testnetTokens: any) => [
  {
    token: "USDC",
    token_hash:
      "0xd6aca1be9729c13d677335161321649cccae6a591554772516700f986f942eaa",
    decimals: 6,
    minimum_withdraw_amount: 0.000001,
    chain_details: testnetTokens,
  },
];

export const EMPTY_LIST: ReadonlyArray<any> = [];

export const EMPTY_OBJECT: Readonly<Record<PropertyKey, any>> = {};

export const EMPTY_OPERATION = () => {};

/**
 * Event name for broadcasting Order Entry estimated liquidation price changes
 * so that UI consumers (e.g. TradingView adapters) can update their visuals
 * without introducing tight coupling between widgets.
 */
export const ORDER_ENTRY_EST_LIQ_PRICE_CHANGE =
  "orderEntry:estLiqPriceChange" as const;

/**
 * Payload for the ORDER_ENTRY_EST_LIQ_PRICE_CHANGE event, carrying the symbol
 * and the latest estimated liquidation price derived from the order form.
 */
export interface OrderEntryEstLiqPriceChangePayload {
  symbol: string;
  estLiqPrice: number | null;
  /** Whether the user is considered active when this payload is emitted (within the active window). */
  isUserActive?: boolean;
}
