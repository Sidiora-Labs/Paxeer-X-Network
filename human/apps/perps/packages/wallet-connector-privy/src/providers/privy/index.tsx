import React from "react";
import { PropsWithChildren } from "react";
import { Chain } from "viem/chains";
import { useWalletConnectorPrivy } from "../../provider";
import { InitPrivy } from "../../types";
import { InitPrivyProvider } from "./initPrivyProvider";
import { PrivyWalletProvider } from "./privyWalletProvider";

export function PrivyWallet(
  props: PropsWithChildren<{ privyConfig?: InitPrivy; initChains: Chain[] }>,
) {
  const { connectorWalletType } = useWalletConnectorPrivy();
  const disabled = connectorWalletType.disablePrivy === true;
  const walletProvider = (
    <PrivyWalletProvider disabled={disabled}>
      {props.children}
    </PrivyWalletProvider>
  );

  if (disabled) {
    return walletProvider;
  }

  return (
    <InitPrivyProvider
      privyConfig={props.privyConfig!}
      initChains={props.initChains}
    >
      {walletProvider}
    </InitPrivyProvider>
  );
}
