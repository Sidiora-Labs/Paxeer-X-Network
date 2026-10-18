import React, { useState } from "react";
import { WalletAdapter } from "@solana/wallet-adapter-base";
import { Connector } from "wagmi";
import { useTranslation } from "@paxeer-network/i18n";
import {
  ChainIcon,
  cn,
  Popover,
  SimpleDialog,
  useScreen,
} from "@paxeer-network/ui";
import { useWalletConnectorPrivy } from "../provider";
import { getWalletIcon } from "../util";

export function RenderPrivyTypeIcon({
  type,
  size,
  black,
}: {
  type: string;
  size: number;
  black?: boolean;
}) {
  if (type === "email") {
    return (
      <img
        src={`/assets/static/sdk/privy/email${
          black ? "-black" : ""
        }.svg`}
        width={size}
      />
    );
  }
  if (type === "google_oauth") {
    return (
      <img
        src="/assets/static/sdk/privy/google.svg"
        width={size}
      />
    );
  }
  if (type === "twitter_oauth") {
    return (
      <img
        src={`/assets/static/sdk/privy/twitter${
          black ? "-black" : ""
        }.svg`}
        width={size}
      />
    );
  }
  if (type === "telegram") {
    return (
      <svg
        width={size}
        height={size}
        viewBox="0 0 18 18"
        fill="none"
        xmlns="http://www.w3.org/2000/svg"
      >
        <path
          d="M2.652 8.12916L15.0683 4.04929C15.6468 3.87358 16.1499 4.16929 15.9587 4.91069L13.8458 13.3962C13.6898 13.9961 13.2672 14.1461 12.6836 13.859L9.46384 11.8362L7.90929 13.109C7.73824 13.2547 7.59234 13.379 7.2603 13.379L7.4867 10.5891L13.4584 5.99923C13.72 5.80638 13.398 5.69495 13.0559 5.8878L5.68061 9.8434L2.50107 8.99914C1.81687 8.81058 1.80178 8.41201 2.652 8.12916Z"
          fill={black ? "black" : "white"}
        />
      </svg>
    );
  }
  return (
    <img
      src={`/assets/static/sdk/privy/email${
        black ? "-black" : ""
      }.svg`}
      width={size}
    />
  );
}
export function RenderWalletIcon({
  connector,
}: {
  connector: Connector | WalletAdapter;
}) {
  const icon = connector.icon
    ? connector.icon
    : getWalletIcon((connector as Connector).type);
  return (
    <div className="oui-w-[18px] oui-h-[18px] oui-flex oui-items-center oui-justify-center">
      <img
        className={cn(
          connector.icon
            ? "oui-w-[12px] oui-h-[12px]"
            : "oui-w-[18px] oui-h-[18px]",
        )}
        src={icon}
        alt={connector.name}
      />
    </div>
  );
}

export function EVMChainPopover({ children }: { children: React.ReactNode }) {
  const { getChainsByNetwork } = useWalletConnectorPrivy();
  const [chains] = useState(getChainsByNetwork("mainnet"));
  const { t } = useTranslation();
  const { isMobile } = useScreen();
  const [open, setOpen] = useState(false);

  if (isMobile) {
    return (
      <>
        <SimpleDialog
          open={open}
          size="xs"
          onOpenChange={setOpen}
          classNames={{
            content: "oui-p-4 oui-z-[99]",
            overlay: "oui-z-[99]",
          }}
          title={t("connector.privy.supportedEvmChain")}
        >
          <div className="oui-p-1 oui-grid oui-grid-cols-2 oui-gap-x-2 oui-gap-y-3 oui-text-2xs oui-text-base-contrast-54">
            {chains.map((item, key) => (
              <div
                key={key}
                className="oui-flex oui-items-center oui-justify-start oui-gap-1"
              >
                <ChainIcon chainId={item.id} size="2xs" />
                <div>{item.name}</div>
              </div>
            ))}
          </div>
        </SimpleDialog>
        <button onClick={() => setOpen(true)}>{children}</button>
      </>
    );
  }

  return (
    <Popover
      content={
        <div>
          <div className="oui-text-2xs oui-font-semibold oui-text-base-contrast">
            {t("connector.privy.supportedEvmChain")}
          </div>
          <div className="oui-mt-3 oui-grid oui-grid-cols-3 oui-gap-x-2 oui-gap-y-3 oui-text-2xs oui-text-base-contrast-54">
            {chains.map((item, key) => (
              <div
                key={key}
                className="oui-flex oui-items-center oui-justify-start oui-gap-1"
              >
                <ChainIcon chainId={item.id} size="2xs" />
                <div>{item.name}</div>
              </div>
            ))}
          </div>
        </div>
      }
      arrow={true}
      contentProps={{
        side: "bottom",
        align: "center",
        className: "oui-p-2 oui-z-[65]",
      }}
    >
      <button>{children}</button>
    </Popover>
  );
}
