import { useTranslation } from "@paxeer-network/i18n";
import { Flex, RwaIcon, Text } from "@paxeer-network/ui";

const RwaIconTab = (props: { iconSize?: number }) => {
  const { iconSize = 12 } = props;
  const { t } = useTranslation();
  return (
    <Flex gap={1}>
      <RwaIcon
        size={iconSize}
        className="group-data-[state=active]:oui-text-base-contrast group-data-[state=inactive]:oui-text-base-contrast-36"
      />
      <Text>{t("markets.tradFi")}</Text>
    </Flex>
  );
};

const RwaTab = () => {
  const { t } = useTranslation();
  return (
    <Flex gap={1}>
      <Text>{t("markets.tradFi")}</Text>
    </Flex>
  );
};

export { RwaIconTab, RwaTab };
