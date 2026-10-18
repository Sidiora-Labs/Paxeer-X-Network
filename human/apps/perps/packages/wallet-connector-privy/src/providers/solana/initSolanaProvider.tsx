import { PropsWithChildren, useCallback, useEffect, useMemo } from "react";
import {
  Adapter,
  WalletAdapterNetwork,
  WalletError,
} from "@solana/wallet-adapter-base";
import { PhantomWalletAdapter } from "@solana/wallet-adapter-phantom";
import { WalletProvider } from "@solana/wallet-adapter-react";
import { useEventEmitter } from "@paxeer-network/hooks";
import { WALLET_CONNECT_ERROR } from "../../connectEvents";
import { useWalletConnectorPrivy } from "../../provider";
import { InitSolana, WalletConnectType } from "../../types";

interface IProps extends PropsWithChildren<InitSolana> {}

export function InitSolanaProvider({
  mainnetRpc,
  devnetRpc,
  wallets: walletsProp,
  onError,
  children,
}: IProps) {
  const ee = useEventEmitter();
  const { network, setSolanaInfo } = useWalletConnectorPrivy();

  const wallets = useMemo(() => {
    return walletsProp ?? [new PhantomWalletAdapter()];
  }, [walletsProp]);

  useEffect(() => {
    let rpcUrl = null;
    if (network === "mainnet") {
      rpcUrl = mainnetRpc ?? null;
    } else {
      rpcUrl = devnetRpc ?? null;
    }
    setSolanaInfo({
      rpcUrl: rpcUrl,
      network:
        network === "mainnet"
          ? WalletAdapterNetwork.Mainnet
          : WalletAdapterNetwork.Devnet,
    });
  }, [network, mainnetRpc, devnetRpc, setSolanaInfo]);

  const handleOnError = useCallback(
    (error: WalletError, adapter?: Adapter) => {
      if (error.name === "WalletAccountError") {
        ee.emit(WALLET_CONNECT_ERROR, {
          walletType: WalletConnectType.SOL,
          message: "Please switch to a wallet with Solana address.",
        });
        return;
      }

      onError?.(error, adapter);
    },
    [ee, onError],
  );

  return (
    <WalletProvider wallets={wallets} onError={handleOnError} autoConnect>
      {children}
    </WalletProvider>
  );
}
