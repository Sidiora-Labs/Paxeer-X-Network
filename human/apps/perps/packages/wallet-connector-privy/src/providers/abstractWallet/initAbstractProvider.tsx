import React, { PropsWithChildren, useMemo } from "react";
import { AbstractWalletProvider as AGWProvider } from "@abstract-foundation/agw-react";
// Use abstract for mainnet
import { abstractTestnet, abstract } from "viem/chains";
import { useWalletConnectorPrivy } from "../../provider";
import { InitAbstract, Network } from "../../types";

export const InitAbstractProvider = (
  props: PropsWithChildren<{ abstractConfig: InitAbstract }>,
) => {
  const { network } = useWalletConnectorPrivy();
  const chain = useMemo(() => {
    if (network === Network.mainnet) {
      return abstract;
    }
    return abstractTestnet;
  }, [network]);
  return (
    <AGWProvider chain={chain} queryClient={props.abstractConfig.queryClient}>
      {props.children}
    </AGWProvider>
  );
};
