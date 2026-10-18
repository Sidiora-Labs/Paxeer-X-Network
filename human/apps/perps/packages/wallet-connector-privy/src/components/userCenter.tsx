import React, { useMemo } from "react";
import { useAccount, useWalletConnector } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import {
  ABSTRACT_CHAIN_ID_MAP,
  ABSTRACT_TESTNET_CHAINID,
  AccountStatusEnum,
} from "@paxeer-network/types";
import {
  Button,
  cn,
  DARK_THEME_CSS_VARS,
  Flex,
  formatAddress,
  LIGHT_THEME_CSS_VARS,
  Text,
  usePaxeerNetworkTheme,
  useScreen,
} from "@paxeer-network/ui";
import { AuthGuard } from "@paxeer-network/ui-connector";
import { usePrivyWallet } from "../providers/privy/privyWalletProvider";
import { RenderPrivyTypeIcon } from "./common";
import { LinkDeviceMobile } from "./linkDevice";
import { shouldUseBlackIcon } from "./userCenter.utils";

export function UserCenter(props: any) {
  const { accountState: state } = props;
  return (
    <RenderUserCenter
      state={state}
      disabledConnect={props.disabledConnect}
      onConnect={props.connect}
    />
  );
}

export const MwebUserCenter = (props: any) => {
  const state = props.accountState ?? props.state;

  return (
    <RenderUserCenter
      state={state}
      disabledConnect={props.disabledConnect}
      onConnect={props.connect}
    />
  );
};

const RenderUserCenter = (props: any) => {
  const { state } = props;
  const { t } = useTranslation();
  const { isMobile } = useScreen();
  const { connect, wallet } = useWalletConnector();
  const { linkedAccount } = usePrivyWallet();
  const { state: accountState, account } = useAccount();
  const { connectedChain } = useWalletConnector();
  const { currentTheme } = usePaxeerNetworkTheme();

  // Keep the login glyph aligned with the theme's explicit contrast token.
  // Custom CSS references cannot be resolved in JavaScript, so fall back to
  // the theme mode when the value is not a literal RGB triplet.
  const iconShouldBeBlack = useMemo(() => {
    const primaryContrast =
      currentTheme?.cssVars?.["--oui-color-primary-contrast"] ??
      (currentTheme?.mode === "light"
        ? LIGHT_THEME_CSS_VARS["--oui-color-primary-contrast"]
        : DARK_THEME_CSS_VARS["--oui-color-primary-contrast"]);
    return shouldUseBlackIcon(primaryContrast, currentTheme?.mode);
  }, [currentTheme]);

  const disabled = state.validating || props.disabledConnect;

  const openWalletCenter = () => {
    const result = props.onConnect ? props.onConnect() : connect();
    result?.catch((error: unknown) => console.error(error));
  };

  const userAddress = useMemo(() => {
    if (
      connectedChain?.id &&
      ABSTRACT_CHAIN_ID_MAP.has(parseInt(connectedChain?.id as string))
    ) {
      return account.getAdditionalInfo()?.AGWAddress;
    }
    return account.address;
  }, [account, connectedChain, accountState]);

  // if (accountStatus.status <= ) {}
  if (state.status === AccountStatusEnum.EnableTradingWithoutConnected) {
    return (
      <Flex className="oui-bg-base-5 oui-px-[7px] oui-rounded-[6px] oui-gap-[6px]">
        <LinkDeviceMobile>
          <Text.formatted
            rule="address"
            className="oui-text-base-contrast oui-text-xs"
          >
            {formatAddress(userAddress!)}
          </Text.formatted>
        </LinkDeviceMobile>
      </Flex>
    );
  }
  if (state.status <= AccountStatusEnum.NotConnected || disabled) {
    if (!props.onConnect) {
      return (
        <AuthGuard
          buttonProps={{
            "data-testid": "oui-testid-nav-bar-connectWallet-btn",
            size: "md",
            className: cn(
              "wallet-connect-button",
              isMobile && "oui-font-semibold oui-px-2",
            ),
          }}
          labels={{
            connectWallet: isMobile
              ? t("connector.connect")
              : t("connector.connectWallet"),
          }}
        />
      );
    }

    return (
      <Button
        data-testid="oui-testid-nav-bar-connectWallet-btn"
        size="md"
        className={cn(
          "wallet-connect-button",
          isMobile && "oui-font-semibold oui-px-2",
        )}
        loading={state.validating}
        disabled={disabled}
        onClick={openWalletCenter}
      >
        {isMobile ? t("connector.connect") : t("connector.connectWallet")}
      </Button>
    );
  }

  if (!wallet) {
    return;
  }
  if (isMobile) {
    return (
      <AuthGuard
        buttonProps={{
          size: "md",
        }}
      >
        <div onClick={openWalletCenter}>
          <Button
            size="md"
            variant="contained"
            data-testid="oui-testid-nav-bar-address-btn"
            className="oui-px-2 oui-flex oui-items-center oui-justify-center oui-gap-1"
          >
            {linkedAccount && (
              <RenderPrivyTypeIcon
                type={linkedAccount.type}
                size={14}
                black={iconShouldBeBlack}
              />
            )}
            <Text.formatted
              rule="address"
              className="oui-text-primary-contrast oui-font-semibold"
            >
              {formatAddress(userAddress!)}
            </Text.formatted>
          </Button>
        </div>
      </AuthGuard>
    );
  }
  return (
    <div onClick={openWalletCenter}>
      <Button
        size="md"
        variant="contained"
        data-testid="oui-testid-nav-bar-address-btn"
        className="oui-flex oui-items-center oui-justify-center oui-gap-2"
      >
        {linkedAccount && (
          <RenderPrivyTypeIcon
            type={linkedAccount.type}
            size={18}
            black={iconShouldBeBlack}
          />
        )}
        <Text.formatted rule="address" className="oui-text-primary-contrast">
          {formatAddress(userAddress!)}
        </Text.formatted>
      </Button>
    </div>
  );
};
