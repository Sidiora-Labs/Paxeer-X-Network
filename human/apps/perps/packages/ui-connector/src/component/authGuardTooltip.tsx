import { PropsWithChildren, useMemo, useState } from "react";
import { useAccount } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { useAppContext } from "@paxeer-network/react-app";
import { AccountStatusEnum } from "@paxeer-network/types";
import { Tooltip } from "@paxeer-network/ui";

type AuthGuardProps = {
  content?: string;
  align?: "center" | "end" | "start";
  alignOffset?: number;
  side?: "top" | "right" | "bottom" | "left";
  sideOffset?: number;
  opactiy?: number;
  tooltip?: {
    connectWallet?: string;
    signIn?: string;
    enableTrading?: string;
    wrongNetwork?: string;
  };
};

const AuthGuardTooltip = (props: PropsWithChildren<AuthGuardProps>) => {
  const { t } = useTranslation();

  const {
    opactiy = 90,
    tooltip = {
      connectWallet: t("connector.setUp.connectWallet.tooltip"),
      signIn: t("connector.setUp.createAccount.tooltip"),
      enableTrading: t("connector.setUp.enableTrading.tooltip"),
      wrongNetwork: t("connector.wrongNetwork.tooltip"),
    },
  } = props;

  const [open, setOpen] = useState(false);
  const { state } = useAccount();
  const isSupport = true;
  const { wrongNetwork } = useAppContext();

  const hint = useMemo(() => {
    if (wrongNetwork) {
      return tooltip?.wrongNetwork;
    }
    switch (state.status) {
      case AccountStatusEnum.NotConnected:
        return tooltip?.connectWallet;
      case AccountStatusEnum.NotSignedIn:
        return tooltip?.signIn;
      case AccountStatusEnum.DisabledTrading:
        return tooltip?.enableTrading;
      case AccountStatusEnum.EnableTrading: {
        return "";
      }
      default:
        return props.content;
    }
  }, [props.content, state, isSupport, tooltip]);

  const newOpacity = useMemo(() => {
    switch (state.status) {
      case AccountStatusEnum.NotConnected:
      case AccountStatusEnum.NotSignedIn:
        return opactiy;
      case AccountStatusEnum.EnableTrading: {
        return undefined;
      }
      default:
        return undefined;
    }
  }, [props.opactiy, state, isSupport]);

  return (
    <Tooltip
      open={hint ? open : false}
      onOpenChange={setOpen}
      content={hint}
      className="oui-text-2xs"
      align={props.align}
      alignOffset={props.alignOffset}
      side={props.side}
      sideOffset={props.sideOffset}
    >
      <div style={{ opacity: newOpacity }}>{props.children}</div>
    </Tooltip>
  );
};

AuthGuardTooltip.displayName = "AuthGuardTooltip";

export { AuthGuardTooltip };
