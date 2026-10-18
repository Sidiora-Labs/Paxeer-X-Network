import React from "react";
import { PrivyClientConfig } from "@privy-io/react-auth";
import { useTranslation } from "@paxeer-network/i18n";
import { cn, Flex, Grid } from "@paxeer-network/ui";
import { useScreen } from "@paxeer-network/ui";
import { useWalletConnectorPrivy } from "../../provider";

const LoginMethodButton = ({
  onClick,
  icon,
  backgroundClassName,
  label,
}: {
  onClick: () => void;
  icon: React.ReactNode;
  backgroundClassName: string;
  label: string;
}) => {
  return (
    <div
      className={cn(
        "oui-flex oui-cursor-pointer oui-items-center oui-justify-center oui-gap-1 oui-rounded-[6px] oui-border-none oui-px-2 oui-py-[11px]",
        backgroundClassName,
      )}
      onClick={onClick}
    >
      {icon}
      <div className="oui-text-2xs oui-text-base-static">{label}</div>
    </div>
  );
};

const RenderLoginMethodsDom = ({
  connect,
  loginMethods,
}: {
  connect: (type: any) => void;
  loginMethods?: PrivyClientConfig["loginMethods"];
}) => {
  const { t } = useTranslation();
  const arr = [];
  if (loginMethods?.includes("email")) {
    arr.push(
      <LoginMethodButton
        onClick={() => connect("email")}
        backgroundClassName="oui-bg-[#333948]"
        icon={
          <img
            src="/assets/static/sdk/privy/email.svg"
            className="oui-size-[18px]"
          />
        }
        label={t("connector.privy.email")}
      />,
    );
  }
  if (loginMethods?.includes("google")) {
    arr.push(
      <LoginMethodButton
        onClick={() => connect("google")}
        backgroundClassName="oui-bg-[#335FFC]"
        icon={
          <img
            src="/assets/static/sdk/privy/google.svg"
            className="oui-size-[18px]"
          />
        }
        label={t("connector.privy.google")}
      />,
    );
  }
  if (loginMethods?.includes("twitter")) {
    arr.push(
      <LoginMethodButton
        onClick={() => connect("twitter")}
        backgroundClassName="oui-bg-[#07080A]"
        icon={
          <img
            src="/assets/static/sdk/privy/twitter.svg"
            className="oui-size-[18px]"
          />
        }
        label={t("connector.privy.twitter")}
      />,
    );
  }
  if (loginMethods?.includes("telegram")) {
    arr.push(
      <LoginMethodButton
        onClick={() => connect("telegram")}
        backgroundClassName="oui-bg-[#3D79A9]"
        icon={
          <svg
            width="18"
            height="18"
            viewBox="0 0 18 18"
            fill="none"
            xmlns="http://www.w3.org/2000/svg"
          >
            <path
              d="M2.652 8.12916L15.0683 4.04929C15.6468 3.87358 16.1499 4.16929 15.9587 4.91069L13.8458 13.3962C13.6898 13.9961 13.2672 14.1461 12.6836 13.859L9.46384 11.8362L7.90929 13.109C7.73824 13.2547 7.59234 13.379 7.2603 13.379L7.4867 10.5891L13.4584 5.99923C13.72 5.80638 13.398 5.69495 13.0559 5.8878L5.68061 9.8434L2.50107 8.99914C1.81687 8.81058 1.80178 8.41201 2.652 8.12916Z"
              className="oui-fill-base-static"
            />
          </svg>
        }
        label={t("connector.privy.telegram")}
      />,
    );
  }
  return arr.map((item, index) => {
    return <div key={index}>{item}</div>;
  });
};

const PrivyConnectAreaMobile = ({
  connect,
}: {
  connect: (type: any) => void;
}) => {
  const { t } = useTranslation();
  const { isMobile, isDesktop } = useScreen();
  const { connectorWalletType, privyConfig } = useWalletConnectorPrivy();
  const loginMethods = privyConfig.loginMethods;
  return (
    <Flex direction="column" className="oui-w-full">
      <Grid cols={2} rows={2} className="oui-w-full" gap={2}>
        <RenderLoginMethodsDom connect={connect} loginMethods={loginMethods} />
      </Grid>
      {(!connectorWalletType.disableWagmi ||
        !connectorWalletType.disableSolana) && (
        <div className="oui-mt-4 oui-h-px oui-w-full oui-bg-line md:oui-mt-5"></div>
      )}
    </Flex>
  );
};

const PrivyConnectAreaDesktop = ({
  connect,
}: {
  connect: (type: any) => void;
}) => {
  const { t } = useTranslation();
  const { connectorWalletType, privyConfig } = useWalletConnectorPrivy();
  const loginMethods = privyConfig.loginMethods;
  return (
    <Flex direction="column" itemAlign={"start"} className="oui-w-full">
      <Grid cols={1} gap={2} className="oui-w-full">
        <RenderLoginMethodsDom connect={connect} loginMethods={loginMethods} />
      </Grid>
      {(!connectorWalletType.disableWagmi ||
        !connectorWalletType.disableSolana) && (
        <div className="oui-mt-4 oui-h-px oui-w-full oui-bg-line md:oui-mt-5"></div>
      )}
    </Flex>
  );
};

export function PrivyConnectArea({
  connect,
}: {
  connect: (type: any) => void;
}) {
  const { isDesktop } = useScreen();
  if (isDesktop) {
    return <PrivyConnectAreaDesktop connect={connect} />;
  }
  return <PrivyConnectAreaMobile connect={connect} />;
}
