import React from "react";
import { useFeeState } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { Flex, Text } from "@paxeer-network/ui";
import { AuthGuard } from "@paxeer-network/ui-connector";

export const RegularFeesUI: React.FC<{ taker: string; maker: string }> = (
  props,
) => {
  const { t } = useTranslation();
  const { taker, maker } = props;

  const originalTrailingFees = (
    <Flex
      itemAlign="center"
      justify="between"
      width={"100%"}
      gap={1}
      className="oui-orderEntry-fees"
    >
      <Flex width={"100%"} itemAlign="center" justify={"between"}>
        <Text className="oui-fees-label oui-truncate" size="2xs">
          {t("common.fees")}
        </Text>
        <AuthGuard
          fallback={() => (
            <Text className="oui-truncate" size="2xs">
              {t("dmm.taker")}: --% / {t("dmm.maker")}: --%
            </Text>
          )}
        >
          <Flex gap={1} className="oui-fees-value-container">
            <Text className="oui-truncate" size="2xs">
              {t("dmm.taker")}:
            </Text>
            <Text size="2xs" className="oui-text-base-contrast-80">
              {taker}
            </Text>
            <Text size="2xs">/</Text>
            <Text className="oui-truncate" size="2xs">
              {t("dmm.maker")}:
            </Text>
            <Text size="2xs" className="oui-text-base-contrast-80">
              {maker}
            </Text>
          </Flex>
        </AuthGuard>
      </Flex>
    </Flex>
  );

  return originalTrailingFees;
};
