export enum SwapMode {
  Single,
  Cross,
}
export enum SwapProcessStatus {
  NONE = -1,
  Bridging = 0,
  BridgeFialed = 1,
  Depositing = 2,
  DepositFailed = 3,
  Done = 4,
}

export type DST = {
  symbol: string;
  address?: string;
  decimals?: number;
  chainId: number;
  network: string;
};

export type MarkPrices = {
  from_token: number;
  native_token: number;
};

export type SymbolInfo = {
  chain: number;
  token: string;
  displayDecimals: number;
  amount: string;
  decimals: number;
};
