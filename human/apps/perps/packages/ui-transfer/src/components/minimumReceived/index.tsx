import React from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { Flex, Text } from "@paxeer-network/ui";

type MinimumReceivedProps = {
  symbol: string;
  value: number | string;
  precision?: number;
};

export const MinimumReceived: React.FC<MinimumReceivedProps> = (props) => {
  const { t } = useTranslation();
  const { value, symbol, precision = 6 } = props;

  return (
    <Flex width="100%" itemAlign="center" justify="between">
      <Flex itemAlign="center" justify="start">
        <Text size="2xs" intensity={36}>
          {t("transfer.swapDeposit.minimumReceived")}
        </Text>
      </Flex>
      <Flex itemAlign="center" justify="end" gap={1}>
        <Text.numeral
          dp={precision}
          size="2xs"
          className="oui-select-none"
          intensity={80}
        >
          {value}
        </Text.numeral>
        <Text size="2xs" className="oui-select-none" intensity={36}>
          {symbol}
        </Text>
      </Flex>
    </Flex>
  );
};
