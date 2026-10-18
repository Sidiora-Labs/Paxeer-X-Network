import React, { useEffect, useState } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { cn } from "@paxeer-network/ui";
import { Tooltip, ChevronDownIcon, ChevronUpIcon } from "@paxeer-network/ui";
import { useWallet } from "../../hooks/useWallet";
import { useWalletConnectorPrivy } from "../../provider";
import { useWagmiWallet } from "../../providers/wagmi/wagmiWalletProvider";
import { WalletConnectType, WalletType } from "../../types";
import { RenderWalletIcon } from "../common";
import { EVMChainPopover } from "../common";
import { MoreIcon } from "../icons";

export function AddEvmWallet() {
  const { t } = useTranslation();
  const [visible, setVisible] = useState(false);
  const { connect } = useWallet();
  const [open, setOpen] = useState(false);
  const { connectors } = useWagmiWallet();
  const { targetWalletType } = useWalletConnectorPrivy();

  useEffect(() => {
    let timer = 0;
    if (targetWalletType === WalletType.EVM) {
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
        content={t("connector.privy.addEvmWallet.tips")}
      >
        <div className="oui-flex oui-items-center oui-justify-center oui-gap-1 ">
          <div className="oui-flex oui-items-center oui-justify-center oui-gap-1">
            <div className="oui-relative oui-flex oui-w-[55px] oui-items-center oui-justify-start">
              <img
                src="/assets/static/sdk/chains.png"
                className="oui-relative oui-z-0 oui-h-[18px]"
              />
              <div className="oui-absolute oui-end-0 oui-flex oui-size-[18px] oui-items-center oui-justify-center oui-rounded-full oui-bg-base-5">
                <EVMChainPopover>
                  <MoreIcon
                    className="oui-relative oui-z-10 oui-size-3 oui-text-base-contrast-54 hover:oui-text-base-contrast"
                    style={{ zIndex: 1 }}
                  />
                </EVMChainPopover>
              </div>
            </div>
          </div>
          <div className="oui-text-2xs oui-font-semibold oui-text-base-contrast-80">
            {t("connector.privy.addEvmWallet")}
          </div>

          <button onClick={() => setVisible(!visible)}>
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
        {connectors.map((item, index) => (
          <div
            key={index}
            className="oui-flex oui-cursor-pointer oui-items-center oui-justify-center  oui-gap-1 oui-bg-base-9 oui-px-2 oui-py-[11px] oui-text-2xs"
            onClick={() =>
              connect({ walletType: WalletConnectType.EVM, connector: item })
            }
          >
            <RenderWalletIcon connector={item} />
            <div className="oui-text-base-contrast">{item.name}</div>
          </div>
        ))}
      </div>
    </div>
  );
}
