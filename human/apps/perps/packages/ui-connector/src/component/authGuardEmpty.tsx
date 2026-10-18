import React from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { AccountStatusEnum } from "@paxeer-network/types";
import { Box, EmptyDataState } from "@paxeer-network/ui";
import { AuthGuard } from "./authGuard";

type AuthGuardProps = {
  hint?: {
    connectWallet?: string;
    signIn?: string;
    enableTrading?: string;
    wrongNetwork?: string;
  };
  status?: AccountStatusEnum;
};

export const AuthGuardEmpty: React.FC<
  React.PropsWithChildren<AuthGuardProps>
> = (props) => {
  const { t } = useTranslation();
  const {
    hint = {
      connectWallet: t("connector.trade.connectWallet.tooltip"),
      signIn: t("connector.trade.createAccount.tooltip"),
      enableTrading: t("connector.trade.enableTrading.tooltip"),
      wrongNetwork: t("connector.wrongNetwork.tooltip"),
    },
    status,
  } = props;

  return (
    <Box my={8}>
      <AuthGuard
        status={status}
        descriptions={{ ...hint, switchChain: hint.wrongNetwork }}
        buttonProps={{
          size: "md",
        }}
      >
        {props.children || <EmptyDataState />}
      </AuthGuard>
    </Box>
  );
};

AuthGuardEmpty.displayName = "AuthGuardEmpty";
