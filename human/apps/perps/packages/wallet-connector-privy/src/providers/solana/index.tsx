import React from "react";
import { useWalletConnectorPrivy } from "../../provider";
import { InitSolana } from "../../types";
import { InitSolanaProvider } from "./initSolanaProvider";
import { SolanaWalletProvider } from "./solanaWalletProvider";

export function SolanaWallet(props: {
  children: React.ReactNode;
  solanaConfig?: InitSolana;
}) {
  const { connectorWalletType } = useWalletConnectorPrivy();
  const disabled = connectorWalletType.disableSolana === true;
  const walletProvider = (
    <SolanaWalletProvider disabled={disabled}>
      {props.children}
    </SolanaWalletProvider>
  );

  if (disabled) {
    return walletProvider;
  }

  return (
    <InitSolanaProvider
      {...(props.solanaConfig ?? { wallets: [], onError: () => {} })}
    >
      {walletProvider}
    </InitSolanaProvider>
  );
}
