import React from "react";
import { useAccount } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { AccountStatusEnum } from "@paxeer-network/types";
import { Flex } from "@paxeer-network/ui";

interface WarningMessageProps {
  crossChainTrans: boolean;
  checkIsBridgeless: boolean;
  qtyGreaterThanMaxAmount: boolean;
  message?: string;
}

export const WithdrawWarningMessage: React.FC<WarningMessageProps> = (
  props,
) => {
  const { crossChainTrans, qtyGreaterThanMaxAmount } = props;
  const { t } = useTranslation();
  const { state } = useAccount();

  const renderContent = () => {
    if (state.status === AccountStatusEnum.NotConnected) {
      return null;
    }
    if (crossChainTrans) {
      return t("transfer.withdraw.crossChain.process");
    }
    if (qtyGreaterThanMaxAmount) {
      return t("transfer.insufficientBalance");
    }

    return props.message;
  };

  const content = renderContent();

  if (!content) {
    return null;
  }

  return (
    <Flex
      my={4}
      className="oui-justify-center oui-text-center oui-text-xs oui-text-warning-darken"
    >
      {content}
    </Flex>
  );
};
