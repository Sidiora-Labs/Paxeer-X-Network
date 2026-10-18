import { useMemo, useState, useEffect, type PropsWithChildren } from "react";
import {
  useAccount,
  useDisconnect,
  useSwitchChain,
} from "@paxeer-network/paxeer-backend/walletRuntime";
import {
  WalletConnectorContext,
  type WalletState,
} from "@paxeer-network/hooks";
import { ChainNamespace } from "@paxeer-network/types";
import { paxeerChain } from "@paxeer-network/paxeer-backend/core/chain";
import type { EvmInitialProps, SolanaInitialProps } from "./types";
import {
  usePaxeer,
  PAXEER_CONNECTOR_ID,
} from "@paxeer-network/paxeer-backend/core/wallet/embedded";
export interface WalletConnectorProviderProps {
  evmInitial?: EvmInitialProps;
  solanaInitial?: SolanaInitialProps;
}
export const WalletConnectorProvider = ({
  children,
}: PropsWithChildren<WalletConnectorProviderProps>) => {
  const auth = usePaxeer();
  const account = useAccount();
  const { disconnectAsync } = useDisconnect();
  const { switchChainAsync, isPending: settingChain } = useSwitchChain();
  const [connecting, setConnecting] = useState(false);
  const [resolved, setResolved] = useState<{
    connector: NonNullable<typeof account.connector>;
    provider: WalletState["provider"];
  } | null>(null);
  useEffect(() => {
    let alive = true;
    setResolved(null);
    const connector = account.connector;
    // Persisted connectors contain metadata only until Wagmi reconnects them.
    // isConnected can also be true during that restoration, so wait for status.
    if (
      account.status === "connected" &&
      typeof connector?.getProvider === "function"
    ) {
      void (async () => {
        try {
          const provider = await connector.getProvider();
          if (
            alive &&
            provider &&
            typeof provider === "object" &&
            "request" in provider &&
            typeof provider.request === "function"
          ) {
            setResolved({
              connector,
              provider: provider as WalletState["provider"],
            });
          }
        } catch {
          // A locked or removed wallet stays disconnected in the native UI.
        }
      })();
    }
    return () => {
      alive = false;
    };
  }, [account.connector, account.status]);
  const provider =
    account.status === "connected" && resolved?.connector === account.connector
      ? resolved?.provider
      : null;
  const wallet = useMemo<WalletState | null>(
    () =>
      account.address && provider
        ? {
            label: account.connector?.name ?? "Paxeer wallet",
            icon: "/pax-white.svg",
            provider,
            accounts: [{ address: account.address }],
            chains: [
              {
                id: account.chainId ?? paxeerChain.id,
                namespace: ChainNamespace.evm,
              },
            ],
          }
        : null,
    [account.address, account.chainId, account.connector, provider],
  );
  const value = useMemo(
    () => ({
      connect: async (options?: {
        autoSelect?: { disableModals?: boolean };
      }) => {
        // WagmiProvider restores saved connections. SDK silent-restore calls
        // must never open an interactive picker while that work is pending.
        if (options?.autoSelect?.disableModals) return wallet ? [wallet] : [];
        setConnecting(true);
        try {
          return await new Promise<WalletState[]>((resolve, reject) =>
            window.dispatchEvent(
              new CustomEvent("paxeer-network:wallet-picker", {
                detail: { resolve, reject },
              }),
            ),
          );
        } finally {
          setConnecting(false);
        }
      },
      disconnect: async () => {
        const embedded = account.connector?.id === PAXEER_CONNECTOR_ID;
        await disconnectAsync();
        if (embedded) await auth.signOut();
        setResolved(null);
        return [];
      },
      connecting,
      setChain: async ({ chainId }: { chainId: number | string }) => {
        await switchChainAsync({ chainId: Number(chainId) });
        return { success: true };
      },
      chains: [
        {
          id: paxeerChain.id,
          label: paxeerChain.name,
          namespace: ChainNamespace.evm,
          rpcUrl: paxeerChain.rpcUrls.default.http[0],
        },
      ],
      wallet,
      connectedChain: wallet?.chains[0] ?? null,
      settingChain,
      namespace: wallet ? ChainNamespace.evm : null,
    }),
    [connecting, disconnectAsync, switchChainAsync, wallet, settingChain],
  );
  return (
    <WalletConnectorContext.Provider value={value as any}>
      {children}
    </WalletConnectorContext.Provider>
  );
};
