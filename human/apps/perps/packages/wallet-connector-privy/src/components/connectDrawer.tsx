import React, { useCallback, useMemo } from "react";
import { useLocalStorage } from "@paxeer-network/hooks";
import { useTranslation, Trans } from "@paxeer-network/i18n";
import { ConnectorKey } from "@paxeer-network/types";
import {
  useScreen,
  cn,
  CloseIcon,
  SimpleSheet,
  Box,
  Flex,
  Text,
  modal,
  CloseSquareFillIcon,
} from "@paxeer-network/ui";
import { useWalletConnectorPrivy } from "../provider";
import { useAbstractWallet } from "../providers/abstractWallet/abstractWalletProvider";
import { usePrivyWallet } from "../providers/privy/privyWalletProvider";
import { useSolanaWallet } from "../providers/solana/solanaWalletProvider";
import { useWagmiWallet } from "../providers/wagmi/wagmiWalletProvider";
import { WalletConnectType, WalletType } from "../types";
import { Drawer } from "./drawer";
import { ArrowRightIcon, ArrowRightLinearGradientIcon } from "./icons";
import { PwaDialog } from "./pwaDilaog";
import { RenderConnector } from "./renderConnector";
import { RenderNonPrivyWallet } from "./renderNonPrivyWallet";
import { RenderPrivyWallet } from "./renderPrivyWallet";

function MyWallet() {
  const [connectorKey, setConnectorKey] = useLocalStorage(ConnectorKey, "");

  return (
    <div>
      {connectorKey === "privy" && <RenderPrivyWallet />}

      {connectorKey !== "privy" && <RenderNonPrivyWallet />}
    </div>
  );
}

export function ConnectDrawer(props: {
  open: boolean;
  onChangeOpen: (open: boolean) => void;
  headerProps?: {
    mobile: React.ReactNode;
  };
}) {
  const { t } = useTranslation();
  const { isConnected: isConnectedPrivy } = usePrivyWallet();
  const { isConnected: isConnectedEvm } = useWagmiWallet();
  const { isConnected: isConnectedSolana } = useSolanaWallet();
  const { isConnected: isConnectedAbstract } = useAbstractWallet();
  const { termsOfUse } = useWalletConnectorPrivy();
  const [connectorKey] = useLocalStorage(ConnectorKey, "");

  const isConnected = useMemo(() => {
    if (connectorKey === WalletConnectType.PRIVY && isConnectedPrivy) {
      return true;
    }
    if (connectorKey !== WalletConnectType.PRIVY) {
      if (isConnectedEvm) {
        return true;
      }
      if (isConnectedSolana) {
        return true;
      }
      if (isConnectedAbstract) {
        return true;
      }
    }
    return false;
  }, [
    isConnectedPrivy,
    isConnectedEvm,
    isConnectedSolana,
    isConnectedAbstract,
    connectorKey,
  ]);

  const { isMobile } = useScreen();

  const showPwaDialog = () => {
    modal.show(PwaDialog);
  };

  const renderHeader = useCallback(() => {
    return (
      <div
        className={cn(
          "oui-font-semibold oui-text-base-contrast-80 ",
          "oui-pb-2 oui-text-[20px]",
          "md:oui-py-0 md:oui-text-base",
        )}
      >
        {isConnected
          ? t("connector.privy.myWallet")
          : t("connector.connectWallet")}
      </div>
    );
  }, [isConnected, t]);
  return (
    <SimpleSheet
      open={props.open}
      onOpenChange={props.onChangeOpen}
      classNames={{
        body: "oui-h-full oui-py-0  oui-border-none oui-relative",
        // overlay: "!oui-bg-base-10/60",
        content: cn(
          "!oui-p-4  !oui-bg-base-9 !oui-border !oui-border-solid !oui-border-line-12",

          isMobile
            ? "oui-inset-y-0 oui-end-0 oui-w-[280px] oui-rounded-none !oui-bg-base-8"
            : "!oui-bottom-[30px] oui-end-3 oui-top-[48px] !oui-h-auto oui-w-[300px] oui-overflow-hidden oui-rounded-[16px] !oui-bg-base-9 ",
        ),
      }}
      contentProps={{ side: "right", closeable: false }}
    >
      {!isMobile && (
        <div
          className="oui-absolute oui-inset-x-[50px] -oui-top-[calc(100vh/2)] oui-z-0 oui-h-screen"
          style={{
            background:
              "conic-gradient(from -41deg at 40.63% 50.41%, rgba(242, 98, 181, 0.00) 125.17920970916748deg, rgba(95, 197, 255, 0.20) 193.4119462966919deg, rgba(255, 172, 137, 0.20) 216.0206937789917deg, rgba(129, 85, 255, 0.20) 236.0708713531494deg, rgba(120, 157, 255, 0.20) 259.95326042175293deg, rgba(159, 115, 241, 0.00) 311.0780096054077deg)",
            filter: "blur(50px)",
          }}
        />
      )}
      <div className="oui-relative oui-z-10 oui-flex oui-h-full oui-flex-col oui-gap-4 md:oui-gap-5">
        <div className="oui-flex oui-flex-none oui-items-center oui-justify-between">
          {renderHeader()}
          <CloseSquareFillIcon
            className="oui-size-5 oui-cursor-pointer oui-text-base-contrast-20 hover:oui-text-base-contrast-80"
            onClick={() => props.onChangeOpen(false)}
          />
        </div>
        {isConnected ? <MyWallet /> : <RenderConnector />}

        {!isConnected && (
          <Flex gap={4} direction="column">
            {isMobile && (
              <Flex
                itemAlign="center"
                justify="between"
                gap={4}
                className="oui-rounded-[8px] oui-px-3 oui-py-2 oui-text-2xs oui-bg-[linear-gradient(270deg,rgb(var(--oui-gradient-brand-end)_/_0.10)_0%,rgb(var(--oui-gradient-brand-start)_/_0.10)_100%)] oui-w-full"
                onClick={() => showPwaDialog()}
              >
                <Text className="oui-bg-[linear-gradient(270deg,rgb(var(--oui-gradient-brand-end))_0%,rgb(var(--oui-gradient-brand-start))_100%)] oui-bg-clip-text oui-text-transparent">
                  {t("connector.privy.pwa.title")}
                </Text>
                <ArrowRightLinearGradientIcon
                  size={20}
                  className="oui-flex-shrink-0"
                />
              </Flex>
            )}
            {termsOfUse && (
              <div className="oui-flex-none oui-text-center oui-text-2xs oui-font-semibold  oui-text-base-contrast-80">
                {/* @ts-ignore */}
                <Trans
                  i18nKey="connector.privy.termsOfUse"
                  components={[
                    <a
                      key="termsOfUse"
                      href={termsOfUse}
                      className="oui-cursor-pointer oui-text-primary oui-underline"
                      target="_blank"
                      rel="noreferrer"
                    />,
                  ]}
                />
              </div>
            )}
          </Flex>
        )}
      </div>
    </SimpleSheet>
  );
}
