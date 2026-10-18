import React, { useEffect, useRef, useState } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { ChainNamespace } from "@paxeer-network/types";
import {
  ChevronDownIcon,
  ChevronUpIcon,
  cn,
  Tooltip,
} from "@paxeer-network/ui";
import { useWallet } from "../../hooks/useWallet";
import { useWalletConnectorPrivy } from "../../provider";
import { useSolanaWallet } from "../../providers/solana/solanaWalletProvider";
import { WalletConnectType, WalletType } from "../../types";
import { RenderWalletIcon } from "../common";

export function AddSolanaWallet() {
  const { t } = useTranslation();
  const { wallets } = useSolanaWallet();
  const { connect } = useWallet();
  const [visible, setVisible] = useState(false);
  const onToggleVisibility = () => {
    setVisible(!visible);
  };
  const { targetWalletType } = useWalletConnectorPrivy();
  const [open, setOpen] = useState(false);
  console.log("-- open and targetNamespace", open, targetWalletType);

  useEffect(() => {
    let timer = 0;
    if (targetWalletType === WalletType.SOL) {
      timer = window.setTimeout(() => {
        setOpen(true);
      }, 200);
    }
    return () => {
      if (timer) {
        window.clearTimeout(timer);
      }
    };
  }, [targetWalletType]);
  useEffect(() => {
    if (open === false) {
      return;
    }
    const timeId = window.setTimeout(() => {
      setOpen(false);
    }, 5000);
    return () => {
      if (timeId) {
        window.clearTimeout(timeId);
      }
    };
  }, [open]);

  return (
    <div className="oui-rounded-[8px] oui-bg-base-10 oui-px-2 oui-py-[11px]">
      <Tooltip
        className="oui-z-[65] oui-max-w-[200px] oui-text-warning-darken"
        open={open}
        content={t("connector.privy.addSolanaWallet.tips")}
      >
        <div className="oui-flex oui-items-center oui-justify-center oui-gap-1 ">
          <img
            src="/assets/static/sdk/solana-logo.png"
            className="oui-size-[15px]"
          />
          <div className="oui-text-2xs oui-font-semibold oui-text-base-contrast-80">
            {t("connector.privy.addSolanaWallet")}
          </div>

          <button onClick={onToggleVisibility}>
            {visible ? (
              <ChevronDownIcon
                size={16}
                opacity={1}
                className="oui-text-base-contrast-36"
              />
            ) : (
              <ChevronUpIcon
                size={16}
                opacity={1}
                className="oui-text-base-contrast"
              />
            )}
          </button>
        </div>
      </Tooltip>
      <div
        className={cn(
          "oui-transition-height oui-grid oui-grid-cols-2 oui-gap-2 oui-overflow-hidden oui-duration-150",
          visible ? "oui-mt-0 oui-max-h-0" : "oui-mt-3 oui-max-h-[400px]",
        )}
      >
        {wallets.map((item, index) => (
          <div
            key={index}
            className="oui-flex oui-cursor-pointer oui-items-center oui-justify-start  oui-gap-1 oui-bg-base-9 oui-px-2 oui-py-[11px]"
            onClick={() =>
              connect({
                walletType: WalletConnectType.SOL,
                walletAdapter: item.adapter,
              })
            }
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
