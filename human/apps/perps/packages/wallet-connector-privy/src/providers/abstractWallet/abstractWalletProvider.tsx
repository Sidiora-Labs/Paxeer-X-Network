import {
  createContext,
  PropsWithChildren,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
} from "react";
import {
  useAbstractClient,
  useGlobalWalletSignerAccount,
  useLoginWithAbstract,
} from "@abstract-foundation/agw-react";
import { useAccount, useConnect } from "wagmi";
import { ConnectedChain, useEventEmitter } from "@paxeer-network/hooks";
import { ChainNamespace } from "@paxeer-network/types";
import { windowGuard } from "@paxeer-network/utils";
import {
  getWalletConnectErrorMessage,
  isWalletConnectCancellation,
  WALLET_CONNECT_ERROR,
  WALLET_CONNECT_PROVIDER_CANCEL,
} from "../../connectEvents";
import { useWalletConnectorPrivy } from "../../provider";
import {
  AbstractChainsMap,
  IWalletState,
  WalletConnectType,
} from "../../types";

interface AbstractWalletContextValue {
  connect: () => void;
  isConnected: boolean;
  disconnect: () => void;
  wallet: IWalletState | null;
  connectedChain: ConnectedChain | undefined;
}

const AbstractWalletContext = createContext<AbstractWalletContextValue | null>(
  null,
);

const disabledAbstractWalletValue: AbstractWalletContextValue = {
  connect: () => {},
  isConnected: false,
  disconnect: () => {},
  wallet: null,
  connectedChain: undefined,
};

export const AbstractWalletProvider = (
  props: PropsWithChildren<{ disabled: boolean }>,
) => {
  if (props.disabled) {
    return (
      <AbstractWalletContext.Provider value={disabledAbstractWalletValue}>
        {props.children}
      </AbstractWalletContext.Provider>
    );
  }

  return (
    <EnabledAbstractWalletProvider>
      {props.children}
    </EnabledAbstractWalletProvider>
  );
};

const EnabledAbstractWalletProvider = (props: PropsWithChildren) => {
  const { network } = useWalletConnectorPrivy();
  const { login, logout } = useLoginWithAbstract();
  const { connect: connectWagmi, connectors } = useConnect();
  const [wallet, setWallet] = useState<IWalletState | null>(null);
  const { data: client } = useAbstractClient();
  const { connector } = useAccount();
  const { address } = useGlobalWalletSignerAccount();
  const ee = useEventEmitter();

  const connect = useCallback(() => {
    const abstractConnector = connectors.find(
      (connector) => connector.id === "xyz.abs.privy",
    );
    if (!abstractConnector) {
      ee.emit(WALLET_CONNECT_ERROR, {
        walletType: WalletConnectType.ABSTRACT,
        message: "Abstract connector not found",
      });
      return;
    }

    try {
      connectWagmi(
        { connector: abstractConnector },
        {
          onError: (error) => {
            if (isWalletConnectCancellation(error)) {
              ee.emit(WALLET_CONNECT_PROVIDER_CANCEL, {
                walletType: WalletConnectType.ABSTRACT,
              });
              return;
            }

            ee.emit(WALLET_CONNECT_ERROR, {
              walletType: WalletConnectType.ABSTRACT,
              message: getWalletConnectErrorMessage(
                error,
                "Failed to connect to Abstract Global Wallet.",
              ),
            });
          },
        },
      );
    } catch (error) {
      if (isWalletConnectCancellation(error)) {
        ee.emit(WALLET_CONNECT_PROVIDER_CANCEL, {
          walletType: WalletConnectType.ABSTRACT,
        });
        return;
      }

      ee.emit(WALLET_CONNECT_ERROR, {
        walletType: WalletConnectType.ABSTRACT,
        message: getWalletConnectErrorMessage(
          error,
          "Failed to connect to Abstract Global Wallet.",
        ),
      });
    }
  }, [connectWagmi, connectors, ee]);

  const disconnect = () => {
    return logout();
  };

  const isConnected = useMemo(() => {
    return !!(client && connector);
  }, [client, connector]);

  const connectedChain = useMemo(() => {
    if (!client || !connector) {
      return;
    }
    return {
      id: AbstractChainsMap.get(network)!,
      namespace: ChainNamespace.evm,
    };
  }, [client, connector, network]);

  const value = useMemo(
    () => ({
      isConnected,
      connect,
      disconnect,
      wallet,
      connectedChain,
    }),
    [connect, disconnect, isConnected, wallet, connectedChain],
  );

  useEffect(() => {
    if (!client || !connector || !address) {
      setWallet(null);
      return;
    }
    connector
      ?.getProvider()
      .then((provider: any) => {
        console.log("xxx abstract wallet in wagmi provider", provider);
        const tempWallet: IWalletState = {
          label: "AGW",
          icon: "",
          provider: {
            ...provider,
            agwWallet: true,
            sendTransaction: async (params: any) => {
              console.log("--- agw wallet sendTransaction", params);
              return client.sendTransaction(params);
            },
            writeContract: async (params: any) => {
              console.log("--- agw wallet writeContract", params);
              return client.writeContract(params);
            },
          },
          accounts: [
            {
              address: address,
            },
          ],
          chains: [
            {
              id: AbstractChainsMap.get(network)!,
              namespace: ChainNamespace.evm,
            },
          ],
          chain: connectedChain,
          additionalInfo: {
            AGWAddress: client.account.address,
          },
        };
        console.log("-- abstract wallet tempWallet", tempWallet);
        setWallet(tempWallet);
      })
      .catch((error) => {
        setWallet(null);
        ee.emit(WALLET_CONNECT_ERROR, {
          walletType: WalletConnectType.ABSTRACT,
          message: getWalletConnectErrorMessage(
            error,
            "Failed to initialize Abstract Global Wallet.",
          ),
        });
      });
  }, [address, client, connectedChain, connector, ee, network]);

  useEffect(() => {
    windowGuard(() => {
      const connection = localStorage.getItem(
        "privy-caw:cm04asygd041fmry9zmcyn5o5:connection",
      );
      if (connection) {
        login();
      }
    });
  }, [login]);
  return (
    <AbstractWalletContext.Provider value={value}>
      {props.children}
    </AbstractWalletContext.Provider>
  );
};

export function useAbstractWallet() {
  const context = useContext(AbstractWalletContext);
  if (!context) {
    throw new Error(
      "useAbstractWallet must be used within a AbstractWalletProvider",
    );
  }
  return context;
}
