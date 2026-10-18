export interface NativeSnapshot {
  address: `0x${string}` | null;
  chainId: number | null;
  markets: any[];
  positions: any[];
  totalVaultUsd: number | null;
  freeCollateral: number | null;
  unrealizedPnl: number | null;
  walletBalances: Record<string, string>;
  dailyPnl?: any[];
  leverage: Record<string, number>;
  connected: boolean;
  marginMode?: "CROSS" | "ISOLATED";
}
let snapshot: NativeSnapshot = {
  address: null,
  chainId: null,
  markets: [],
  positions: [],
  totalVaultUsd: null,
  freeCollateral: null,
  unrealizedPnl: null,
  walletBalances: {},
  leverage: {},
  connected: false,
};
const listeners = new Set<() => void>();
export const getNativeSnapshot = () => snapshot;
export function updateNativeSnapshot(update: Partial<NativeSnapshot>) {
  if (
    "address" in update &&
    update.address?.toLowerCase() !== snapshot.address?.toLowerCase()
  )
    snapshot = {
      ...snapshot,
      walletBalances: {},
      positions: [],
      totalVaultUsd: null,
      freeCollateral: null,
      unrealizedPnl: null,
      dailyPnl: undefined,
      leverage: {},
      marginMode: undefined,
    };
  snapshot = { ...snapshot, ...update };
  listeners.forEach((listener) => listener());
}
export function subscribeNativeSnapshot(listener: () => void) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}
