import { CardTitle, Flex } from "@paxeer-network/ui";
import { useTranslation } from "@paxeer-network/i18n";

export const AssetsChartHeader = () => {
  const { t } = useTranslation();

  return (
    <Flex justify={"between"}>
      <CardTitle>{t("common.assets")}</CardTitle>
    </Flex>
  );
};
