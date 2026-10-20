/**
 * Wallet state management with Zustand — backed by wagmi.
 *
 * This is a thin compatibility layer that exposes the same API surface
 * (`status`, `lock()`, etc.) that the rest of the app expects, but
 * under the hood it's driven by wagmi's injected wallet connection.
 *
 * No more wallet-core, PINs, mnemonics, or private keys.
 */

import { create } from "zustand";
import { devtools } from "zustand/middleware";

type WalletStatus = "no_wallet" | "locked" | "unlocked";

interface WalletState {
  status: WalletStatus;
  address: `0x${string}` | null;
  chainId: number | null;
  isLoading: boolean;
  error: string | null;
  connectModalOpen: boolean;

  initialize: () => Promise<void>;
  setConnected: (address: `0x${string}`, chainId: number) => void;
  setDisconnected: () => void;
  lock: () => void;
  openConnectModal: () => void;
  closeConnectModal: () => void;
  clearError: () => void;
}

export const useWalletStore = create<WalletState>()(
  devtools(
    (set) => ({
      status: "no_wallet",
      address: null,
      chainId: null,
      isLoading: false,
      error: null,
      connectModalOpen: false,

      initialize: async () => {
        // wagmi handles reconnection automatically via WagmiProvider
        // This is now a no-op — status is synced via WalletSyncer component
      },

      setConnected: (address, chainId) => {
        set({
          status: "unlocked",
          address,
          chainId,
          isLoading: false,
          error: null,
        });
      },

      setDisconnected: () => {
        set({
          status: "no_wallet",
          address: null,
          chainId: null,
          isLoading: false,
        });
      },

      lock: () => {
        // In injected model, "lock" = disconnect
        // Actual disconnect is triggered by the component calling wagmi's disconnect
        set({
          status: "no_wallet",
          address: null,
          chainId: null,
        });
      },

      openConnectModal: () => set({ connectModalOpen: true }),
      closeConnectModal: () => set({ connectModalOpen: false }),
      clearError: () => set({ error: null }),
    }),
    { name: "WalletStore" }
  )
);
