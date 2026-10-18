import React from "react";
import { WalletAdapter } from "@solana/wallet-adapter-base";
import { cn, useScreen } from "@paxeer-network/ui";
import { useSolanaWallet } from "../../providers/solana/solanaWalletProvider";
import { RenderWalletIcon } from "../common";

export function SOLConnectArea({
  connect,
}: {
  connect: (walletAdapter: WalletAdapter) => void;
}) {
  const { wallets } = useSolanaWallet();
  const { isMobile } = useScreen();
  return (
    <div>
      <div className="oui-mb-2 oui-text-sm oui-font-semibold oui-text-base-contrast-80">
        Solana
      </div>
      <div className="oui-grid oui-grid-cols-2 oui-gap-2">
        {wallets.map((item, key) => (
          <div
            key={key}
            className={cn(
              " oui-flex oui-flex-1 oui-cursor-pointer oui-items-center oui-justify-start oui-gap-1 oui-rounded-[6px]  oui-px-2 oui-py-[11px]",
              isMobile ? "oui-bg-base-5" : "oui-bg-base-10",
            )}
            onClick={() => connect(item.adapter)}
          >
            <RenderWalletIcon connector={item.adapter} />
            <div className="oui-text-2xs oui-text-base-contrast">
              {item.adapter.name}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
