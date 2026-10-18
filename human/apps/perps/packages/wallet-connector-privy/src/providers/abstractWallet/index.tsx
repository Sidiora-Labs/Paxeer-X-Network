import React, { PropsWithChildren } from "react";
import { useWalletConnectorPrivy } from "../../provider";
import { InitAbstract } from "../../types";
import { AbstractWalletProvider } from "./abstractWalletProvider";
import { InitAbstractProvider } from "./initAbstractProvider";

export function AbstractWallet({
  abstractConfig,
  children,
}: PropsWithChildren<{ abstractConfig?: InitAbstract }>) {
  const { connectorWalletType } = useWalletConnectorPrivy();
  const disabled = connectorWalletType.disableAGW === true || !abstractConfig;
  const walletProvider = (
    <AbstractWalletProvider disabled={disabled}>
      {children}
    </AbstractWalletProvider>
  );

  if (disabled) {
    return walletProvider;
  }

  return (
    <InitAbstractProvider abstractConfig={abstractConfig}>
      {walletProvider}
    </InitAbstractProvider>
  );
}
