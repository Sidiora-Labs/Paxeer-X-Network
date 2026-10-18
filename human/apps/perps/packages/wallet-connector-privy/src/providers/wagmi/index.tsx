import React from "react";
import { Chain } from "viem/chains";
import { EMPTY_OBJECT } from "@paxeer-network/types";
import { useWalletConnectorPrivy } from "../../provider";
import { InitWagmi } from "../../types";
import { InitWagmiProvider } from "./initWagmiProvider";
import { WagmiWalletProvider } from "./wagmiWalletProvider";

export const WagmiWallet: React.FC<
  React.PropsWithChildren<{ wagmiConfig?: InitWagmi; initChains: Chain[] }>
> = (props) => {
  const { connectorWalletType } = useWalletConnectorPrivy();
  const disabled = connectorWalletType.disableWagmi === true;
  const walletProvider = (
    <WagmiWalletProvider disabled={disabled}>
      {props.children}
    </WagmiWalletProvider>
  );

  if (disabled) {
    return walletProvider;
  }

  return (
    <InitWagmiProvider
      initChains={props.initChains}
      wagmiConfig={props.wagmiConfig ?? EMPTY_OBJECT}
    >
      {walletProvider}
    </InitWagmiProvider>
  );
};
