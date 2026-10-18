import React, {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
} from "react";
import {
  Connector,
  useAccount,
  useConnect,
  useDisconnect,
  useSwitchChain,
} from "wagmi";
import { useEventEmitter } from "@paxeer-network/hooks";
import { ChainNamespace } from "@paxeer-network/types";
import {
  getWalletConnectErrorMessage,
  isWalletConnectCancellation,
  WALLET_CONNECT_ERROR,
  WALLET_CONNECT_PROVIDER_CANCEL,
} from "../../connectEvents";
import { WalletConnectType } from "../../types";

interface WagmiWalletContextValue {
  connectors: Connector[];
  connect: (args: any) => void;
  wallet: any;
  connectedChain: { id: number; namespace: ChainNamespace } | null;
  setChain: (chainId: number) => Promise<any>;
  disconnect: () => void;
  isConnected: boolean;
}

const WagmiWalletContext = createContext<WagmiWalletContextValue | null>(null);

const disabledWagmiWalletValue: WagmiWalletContextValue = {
  connectors: [],
  connect: () => {},
  wallet: undefined,
  connectedChain: null,
  setChain: () =>
    Promise.reject(new Error("Wagmi wallet connector is disabled")),
  disconnect: () => {},
  isConnected: false,
};

export const WagmiWalletProvider: React.FC<{
  children: React.ReactNode;
  disabled: boolean;
}> = ({ children, disabled }) => {
  if (disabled) {
    return (
      <WagmiWalletContext.Provider value={disabledWagmiWalletValue}>
        {children}
      </WagmiWalletContext.Provider>
    );
  }

  return <EnabledWagmiWalletProvider>{children}</EnabledWagmiWalletProvider>;
};

const EnabledWagmiWalletProvider: React.FC<{ children: React.ReactNode }> = ({
  children,
}) => {
  const [wallet, setWallet] = useState<undefined | any>(undefined);
  const { connect: connectWagmi, connectors: wagmiConnectors } = useConnect();
  const { disconnect } = useDisconnect();
  const { connector, isConnected, address, chainId } = useAccount();
  const { switchChain } = useSwitchChain();
  const ee = useEventEmitter();

  const connect = useCallback(
    (args: any) => {
      connectWagmi(args, {
        onError: (error) => {
          if (isWalletConnectCancellation(error)) {
            ee.emit(WALLET_CONNECT_PROVIDER_CANCEL, {
              walletType: WalletConnectType.EVM,
            });
            return;
          }

          ee.emit(WALLET_CONNECT_ERROR, {
            walletType: WalletConnectType.EVM,
            message: getWalletConnectErrorMessage(
              error,
              "Failed to connect to the wallet.",
            ),
          });
        },
      });
    },
    [connectWagmi, ee],
  );

  const connectedChain = useMemo(() => {
    if (chainId) {
      return {
        id: chainId,
        namespace: ChainNamespace.evm,
      };
    }
    return null;
  }, [chainId]);

  const setChain = useCallback(
    (chainId: number) => {
      return new Promise((resolve, reject) => {
        switchChain(
          { chainId },
          {
            onSuccess: () => resolve(true),
            onError: (e) => {
              console.log("-- switch chain error", e);
              return reject(e);
            },
          },
        );
      });
    },
    [switchChain],
  );

  useEffect(() => {
    if (!connector || !isConnected) {
      setWallet(undefined);
      return;
    }
    const providerPromise = connector.getProvider?.();
    if (!providerPromise) {
      setWallet(undefined);
      ee.emit(WALLET_CONNECT_ERROR, {
        walletType: WalletConnectType.EVM,
        message: "Failed to initialize the wallet provider.",
      });
      return;
    }

    providerPromise
      .then((provider) => {
        setWallet({
          label: connector.name,
          icon: "",
          provider: provider,
          accounts: [
            {
              address: address,
            },
          ],
          chains: [
            {
              id: chainId,
              namespace: ChainNamespace.evm,
            },
          ],
          chain: connectedChain,
        });
      })
      .catch((error) => {
        setWallet(undefined);
        ee.emit(WALLET_CONNECT_ERROR, {
          walletType: WalletConnectType.EVM,
          message: getWalletConnectErrorMessage(
            error,
            "Failed to initialize the wallet provider.",
          ),
        });
      });
  }, [address, chainId, connectedChain, connector, ee, isConnected]);

  const connectors = useMemo(() => {
    return wagmiConnectors
      .filter((connector: any) => connector.id !== "injected")
      .sort((a: any, b: any) =>
        a.type === "injected" ? -1 : 1,
      ) as Connector[];
  }, [wagmiConnectors]);

  const value = useMemo(
    () => ({
      connectors,
      connect,
      wallet,
      connectedChain,
      setChain,
      disconnect,
      isConnected,
    }),
    [
      connectors,
      connect,
      wallet,
      connectedChain,
      setChain,
      disconnect,
      isConnected,
    ],
  );

  return (
    <WagmiWalletContext.Provider value={value}>
      {children}
    </WagmiWalletContext.Provider>
  );
};

export function useWagmiWallet() {
  const context = useContext(WagmiWalletContext);
  if (!context) {
    throw new Error("useWagmiWallet must be used within a WagmiWalletProvider");
  }
  return context;
}
