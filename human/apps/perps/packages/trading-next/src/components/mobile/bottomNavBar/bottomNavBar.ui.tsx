import { FC, SVGProps, useState } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { AccountStatusEnum } from "@paxeer-network/types";
import { Flex, SimpleDialog, Text } from "@paxeer-network/ui";
import { ScanQRCodeWidget } from "@paxeer-network/ui-scaffold";
import { MobileAccountMenuExtension } from "./account/account.widget";
import { BalanceWidget } from "./balance";
import { BottomNavBarState } from "./bottomNavBar.script";
import { ChainWidget } from "./chain";

export const BottomNavBar: FC<BottomNavBarState> = (props) => {
  const renderContent = () => {
    if (props.wrongNetwork) {
      return null;
    }

    if (
      !props.disabledConnect &&
      props.status === AccountStatusEnum.EnableTradingWithoutConnected
    ) {
      return <LinkDevice onDisconnect={props.onDisconnect} />;
    }

    return <ChainWidget />;
  };

  const showScanQRCode =
    !props.disabledConnect &&
    props.status !== AccountStatusEnum.EnableTradingWithoutConnected &&
    props.status < AccountStatusEnum.EnableTrading;

  return (
    <div className="oui-bottomNavBar oui-bg-base-9 oui-border-t oui-border-line-4">
      <Flex
        height={64}
        gap={1}
        justify={"end"}
        itemAlign={"center"}
        className="oui-px-[14px]"
        // style={{
        //   height: "calc(64px + env(safe-area-inset-bottom))"
        // }}
      >
        <BalanceWidget />
        <Flex gap={2}>
          {showScanQRCode && <ScanQRCodeWidget />}
          {renderContent()}
          <MobileAccountMenuExtension />
        </Flex>
      </Flex>
      <div className="oui-h-[env(safe-area-inset-bottom)]" />
    </div>
  );
};

type LinkDeviceProps = {
  onDisconnect: () => void;
};

const LinkDevice: FC<LinkDeviceProps> = (props) => {
  const [open, setOpen] = useState(false);
  const { t } = useTranslation();

  const showDialog = () => {
    setOpen(true);
  };

  const hideDialog = () => {
    setOpen(false);
  };

  return (
    <>
      <SimpleDialog
        open={open}
        onOpenChange={setOpen}
        title={t("common.tips")}
        size="xs"
        actions={{
          secondary: {
            label: t("common.cancel"),
            onClick: hideDialog,
            size: "md",
            fullWidth: true,
          },
          primary: {
            label: t("connector.disconnect"),
            onClick: async () => {
              await props.onDisconnect();
              hideDialog();
            },
            size: "md",
            variant: "outlined",
            color: "danger",
          },
        }}
      >
        <Text intensity={54} size="sm">
          {t("linkDevice.scanQRCode.connected.description")}
        </Text>
      </SimpleDialog>
      <Flex
        className="oui-text-base-contrast oui-px-[6px]"
        intensity={500}
        height={28}
        r="md"
        onClick={showDialog}
      >
        <DesktopIcon />
        <Dot />
        <LinkIcon />
        <Dot />
        <MobileIcon />
      </Flex>
    </>
  );
};

const Dot = () => {
  return (
    <Flex className="oui-gap-x-[1px] oui-px-[1px]">
      <DotIcon />
      <DotIcon />
      <DotIcon />
    </Flex>
  );
};

const DesktopIcon: FC<SVGProps<SVGSVGElement>> = (props) => (
  <svg
    width="18"
    height="18"
    viewBox="0 0 18 18"
    fill="currentColor"
    xmlns="http://www.w3.org/2000/svg"
    {...props}
  >
    <path d="M4.5 2.93a2.25 2.25 0 0 0-2.25 2.25v5.25a.75.75 0 0 0-.75.75v1.5c0 1.272.93 2.25 2.25 2.25h10.5c1.318 0 2.25-.978 2.25-2.25v-1.5a.75.75 0 0 0-.75-.75V5.18a2.25 2.25 0 0 0-2.25-2.25zm0 1.5h9a.75.75 0 0 1 .75.75v5.25H3.75V5.18a.75.75 0 0 1 .75-.75M3 11.93h12v.75c0 .46-.277.75-.75.75H3.75c-.474 0-.75-.29-.75-.75z" />
  </svg>
);

const MobileIcon: FC<SVGProps<SVGSVGElement>> = (props) => (
  <svg
    width="18"
    height="18"
    viewBox="0 0 18 18"
    fill="currentColor"
    xmlns="http://www.w3.org/2000/svg"
    {...props}
  >
    <path d="M13.498 3.715a2.25 2.25 0 0 0-2.25-2.25h-4.5a2.25 2.25 0 0 0-2.25 2.25v10.5a2.25 2.25 0 0 0 2.25 2.25h4.5a2.25 2.25 0 0 0 2.25-2.25zm-1.5 0v9.75h-6v-9.75a.75.75 0 0 1 .75-.75h4.5a.75.75 0 0 1 .75.75m-2.25 11.25a.75.75 0 1 1-1.5 0 .75.75 0 0 1 1.5 0" />
  </svg>
);

const LinkIcon: FC<SVGProps<SVGSVGElement>> = (props) => (
  <svg
    width="12"
    height="12"
    viewBox="0 0 12 12"
    fill="none"
    xmlns="http://www.w3.org/2000/svg"
  >
    <path
      fillRule="evenodd"
      clipRule="evenodd"
      d="M1.007 6a5 5 0 1 1 10 0 5 5 0 0 1-10 0m7.486-2.344A.6.6 0 0 1 8.91 3.5c.15 0 .305.048.418.156a.55.55 0 0 1 0 .798L5.254 8.337a.62.62 0 0 1-.837 0L2.67 6.673a.55.55 0 0 1 0-.798.62.62 0 0 1 .837 0l1.329 1.266z"
      fill="url(#a)"
    />
    <defs>
      <linearGradient
        id="a"
        x1="11.007"
        y1="5.999"
        x2="1.007"
        y2="5.999"
        gradientUnits="userSpaceOnUse"
      >
        <stop stopColor="rgb(var(--oui-gradient-brand-end))" />
        <stop offset="1" stopColor="rgb(var(--oui-gradient-brand-start))" />
      </linearGradient>
    </defs>
  </svg>
);

const DotIcon: FC<SVGProps<SVGSVGElement>> = (props) => (
  <svg
    width="2"
    height="2"
    viewBox="0 0 2 2"
    fill="none"
    xmlns="http://www.w3.org/2000/svg"
  >
    <path
      d="M.667.334a.667.667 0 1 1 0 1.333.667.667 0 0 1 0-1.333"
      fill="url(#a)"
    />
    <defs>
      <linearGradient
        id="a"
        x1="1.333"
        y1="1.001"
        x2="0"
        y2="1.001"
        gradientUnits="userSpaceOnUse"
      >
        <stop stopColor="rgb(var(--oui-gradient-brand-end))" />
        <stop offset="1" stopColor="rgb(var(--oui-gradient-brand-start))" />
      </linearGradient>
    </defs>
  </svg>
);
