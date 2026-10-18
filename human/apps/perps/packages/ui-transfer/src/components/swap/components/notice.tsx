import { FC, useMemo, ReactNode } from "react";
import { useTranslation, Trans } from "@paxeer-network/i18n";
import {
  isWalletChainChangePendingResult,
  NetworkId,
} from "@paxeer-network/types";
import { Box, Text } from "@paxeer-network/ui";
import { modal } from "@paxeer-network/ui";
import { ChainSelectorDialogId } from "@paxeer-network/ui-chain-selector";

type NoticeProps = {
  message?: string | ReactNode;
  needSwap?: boolean;
  needCrossSwap?: boolean;
  wrongNetwork?: boolean;
  networkId?: NetworkId;
};

export const Notice: FC<NoticeProps> = (props) => {
  const { message, needSwap, needCrossSwap, wrongNetwork, networkId } = props;
  const { t } = useTranslation();

  const showChainSelect = () => {
    void modal.show(ChainSelectorDialogId, { networkId }).catch((error) => {
      if (error === "cancel" || isWalletChainChangePendingResult(error)) {
        return;
      }
      console.error("[switchChain error]", error);
    });
  };

  const content = useMemo(() => {
    if (wrongNetwork) {
      return t("connector.wrongNetwork.tooltip");
    }

    if (message) {
      return message;
    }

    if (needCrossSwap) {
      return (
        <Text>
          {/* @ts-ignore */}
          <Trans
            i18nKey="transfer.swapDeposit.crossSwap.notice"
            components={[
              <Text
                key="0"
                className="oui-cursor-pointer"
                color="primaryLight"
                onClick={showChainSelect}
              />,
            ]}
          />
        </Text>
      );
    }

    if (needSwap) {
      return t("transfer.swapDeposit.swap.notice");
    }
  }, [message, needSwap, needCrossSwap, wrongNetwork, t]);

  if (content) {
    return (
      <Box
        mb={3}
        className="oui-text-center oui-text-xs oui-text-warning-darken"
      >
        {content}
      </Box>
    );
  }

  return null;
};
