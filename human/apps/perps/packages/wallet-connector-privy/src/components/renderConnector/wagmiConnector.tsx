import React from "react";
import { Connector } from "wagmi";
import { useScreen, cn } from "@paxeer-network/ui";
import { useWalletConnectorPrivy } from "../../provider";
import { useWagmiWallet } from "../../providers/wagmi/wagmiWalletProvider";
import { RenderWalletIcon } from "../common";

export function EVMConnectArea({
  connect,
}: {
  connect: (type: Connector) => void;
}) {
  const { connectors } = useWagmiWallet();
  const { setOpenConnectDrawer } = useWalletConnectorPrivy();
  const { isMobile } = useScreen();
  const onConnect = (item: Connector) => {
    if (isMobile) {
      if (item.id === "walletConnect") {
        setOpenConnectDrawer(false);
      }
    }
    connect(item);
  };
  return (
    <div className="">
      <div className="oui-mb-2 oui-text-sm oui-font-semibold oui-text-base-contrast-80">
        EVM
      </div>
      <div className="oui-grid oui-grid-cols-2 oui-gap-2">
        {connectors.map((item, key) => (
          <div
            key={key}
            className={cn(
              " oui-flex oui-flex-1 oui-cursor-pointer oui-items-center oui-justify-start oui-gap-1 oui-rounded-[6px]  oui-px-2 oui-py-[11px]",
              isMobile ? "oui-bg-base-5" : "oui-bg-base-10",
            )}
            onClick={() => onConnect(item)}
          >
            <RenderWalletIcon connector={item} />
            <div className="oui-text-2xs oui-text-base-contrast">
              {item.name}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
