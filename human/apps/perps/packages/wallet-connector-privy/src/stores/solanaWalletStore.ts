// stores/solanaWalletStore.ts
import { WalletAdapterNetwork, WalletName } from "@solana/wallet-adapter-base";
import { create } from "zustand";
import { ChainNamespace } from "@paxeer-network/types";
import { Network, SolanaChainsMap } from "../types";

interface WalletAccount {
  address: string;
}

interface WalletChain {
  id: number;
  namespace: ChainNamespace;
}

interface WalletProvider {
  rpcUrl: string | null;
  network: WalletAdapterNetwork;
  signMessage: any;
  signTransaction: any;
  sendTransaction: any;
}

interface Wallet {
  label: string;
  icon: string;
  provider: WalletProvider;
  accounts: WalletAccount[];
  chains: WalletChain[];
  chain: WalletChain;
}

interface WalletMethods {
  select: (walletName: WalletName) => void;
  connectSolana: () => Promise<void>;
  walletSolana: any;
  publicKey: any;
  connecting: boolean;
  signMessage: any;
  signTransaction: any;
  sendTransaction: any;
  disconnectSolana: () => Promise<void>;
  network: Network;
  solanaInfo: any;
  syncLedgerAddress: (address: string, adapterName: string) => void;
}

interface SolanaWalletState {
  // State
  wallet: Wallet | null;
  isConnecting: boolean;
  error: Error | null;
  isManual: boolean;
  walletMethods: WalletMethods | null;
  pendingWalletName: string | null;

  // Methods
  connect: (walletName: string) => Promise<Wallet>;
  disconnect: () => Promise<void>;
  setWallet: (wallet: Wallet | null) => void;
  setError: (error: Error | null) => void;
  setIsManual: (isManual: boolean) => void;
  setWalletMethods: (methods: WalletMethods) => void;
  setPendingWalletName: (name: string | null) => void;
}

export const useSolanaWalletStore = create<SolanaWalletState>((set, get) => ({
  wallet: null,
  isConnecting: false,
  error: null,
  isManual: false,
  walletMethods: null,
  pendingWalletName: null,

  connect: async (walletName: string) => {
    const { setIsManual, walletMethods } = get();

    if (!walletMethods) {
      throw new Error("Wallet methods not initialized");
    }

    setIsManual(true);
    set({ pendingWalletName: walletName });

    try {
      set({ isConnecting: true, error: null });

      const { walletSolana, select } = walletMethods;

      select(walletName as WalletName);
      await new Promise<void>((resolve) => {
        const checkWallet = () => {
          const currentWallet = get().walletMethods?.walletSolana;
          console.log("currentWallet", currentWallet);
          if (currentWallet) {
            resolve();
          } else {
            setTimeout(checkWallet, 500);
          }
        };
        checkWallet();
      });

      const updatedWalletMethods = get().walletMethods;
      if (!updatedWalletMethods) {
        throw new Error("Wallet methods not initialized");
      }

      if (!updatedWalletMethods.publicKey) {
        await updatedWalletMethods.connectSolana();
      }

      const lastestWalletMethods = get().walletMethods;
      if (!lastestWalletMethods) {
        throw new Error("Wallet methods not initialized");
      }
      if (!lastestWalletMethods.publicKey) {
        throw new Error("Wallet not connected");
      }

      const userAddress = lastestWalletMethods.publicKey.toBase58();
      const adapterName = lastestWalletMethods.walletSolana.adapter.name;
      lastestWalletMethods.syncLedgerAddress(userAddress, adapterName);

      const wallet: Wallet = {
        label: updatedWalletMethods.walletSolana.adapter.name,
        icon: "",
        provider: {
          rpcUrl: lastestWalletMethods.solanaInfo?.rpcUrl ?? null,
          network:
            lastestWalletMethods.solanaInfo?.network ??
            WalletAdapterNetwork.Devnet,
          signMessage: lastestWalletMethods.signMessage,
          signTransaction: lastestWalletMethods.signTransaction,
          sendTransaction: lastestWalletMethods.sendTransaction,
        },
        accounts: [
          {
            address: userAddress,
          },
        ],
        chains: [
          {
            id: SolanaChainsMap.get(lastestWalletMethods.network)!,
            namespace: ChainNamespace.solana,
          },
        ],
        chain: {
          id: SolanaChainsMap.get(lastestWalletMethods.network)!,
          namespace: ChainNamespace.solana,
        },
      };

      set({
        wallet,
        isConnecting: false,
        isManual: false,
        pendingWalletName: null,
      });

      return wallet;
    } catch (error) {
      set({
        error: error as Error,
        isConnecting: false,
        isManual: false,
        pendingWalletName: null,
      });
      throw error;
    }
  },

  disconnect: async () => {
    const { walletMethods } = get();

    if (!walletMethods) {
      throw new Error("Wallet methods not initialized");
    }

    try {
      await walletMethods.disconnectSolana();
      set({ wallet: null, error: null });
    } catch (error) {
      set({ error: error as Error });
      throw error;
    }
  },

  setWallet: (wallet: Wallet | null) => set({ wallet }),
  setError: (error: Error | null) => set({ error }),
  setIsManual: (isManual: boolean) => set({ isManual }),
  setWalletMethods: (methods: WalletMethods) => set({ walletMethods: methods }),
  setPendingWalletName: (name: string | null) =>
    set({ pendingWalletName: name }),
}));
