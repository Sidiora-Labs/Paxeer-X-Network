import React, { useEffect, useState } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { ChevronDownIcon, ChevronUpIcon, cn } from "@paxeer-network/ui";
import { Tooltip } from "@paxeer-network/ui";
import { useWallet } from "../../hooks/useWallet";
import { useWalletConnectorPrivy } from "../../provider";
import { WalletConnectType, WalletType } from "../../types";
import { PrivyConnectorImagePath } from "../../util";

export function AddAbstractWallet() {
  const [visible, setVisible] = useState(false);
  const onToggleVisibility = () => {
    setVisible(!visible);
  };
  const { connect } = useWallet();
  const { targetWalletType } = useWalletConnectorPrivy();
  const [open, setOpen] = useState(false);
  const { t } = useTranslation();

  useEffect(() => {
    let timer = 0;
    if (targetWalletType === WalletType.ABSTRACT) {
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
        content={t("connector.privy.addAbstractWallet.tips")}
      >
        <div className="oui-flex oui-items-center oui-justify-center oui-gap-1 ">
          <img
            src={`${PrivyConnectorImagePath}/abstract-transparent.png`}
            className="oui-size-[15px]"
          />
          <div className="oui-text-2xs oui-font-semibold oui-text-base-contrast-80">
            {t("connector.privy.addAbstractWallet")}
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
        <div
          className="oui-flex oui-cursor-pointer oui-items-center oui-justify-start  oui-gap-1 oui-bg-base-9 oui-px-2 oui-py-[11px]"
          onClick={() =>
            connect({
              walletType: WalletConnectType.ABSTRACT,
            })
          }
        >
          <img
            className={cn("oui-size-[12px]")}
            src={`${PrivyConnectorImagePath}/abstract.png`}
            alt="abstract wallet"
          />
          <div className="oui-text-2xs oui-text-base-contrast">Abstract</div>
        </div>
      </div>
    </div>
  );
}
