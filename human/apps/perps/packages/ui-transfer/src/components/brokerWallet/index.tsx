import { FC, useMemo } from "react";
import { useConfig } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { useAppConfig } from "@paxeer-network/react-app";
import { Flex, Text } from "@paxeer-network/ui";

export const BrokerWallet: FC = () => {
  const { t } = useTranslation();
  const { appIcons } = useAppConfig();
  const brokerName = useConfig("brokerName");

  const icon = useMemo(() => {
    const { secondary } = appIcons || {};

    if (secondary?.img) {
      return <img src={secondary?.img} className="oui-size-5" />;
    }

    if (secondary?.component) {
      return <>{secondary.component}</>;
    }

    return null;
  }, [appIcons]);

  return (
    <Flex justify="between">
      <Text size="sm" intensity={98}>
        {t("transfer.brokerAccount", { brokerName })}
      </Text>
      {icon}
    </Flex>
  );
};
