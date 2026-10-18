import { useGetRwaSymbolOpenStatus } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { Box, Tips, cn, Text } from "@paxeer-network/ui";

export const RwaStatusTag = ({ symbol }: { symbol: string }) => {
  const { isRwa, open } = useGetRwaSymbolOpenStatus(symbol);
  const { t } = useTranslation();

  if (!isRwa) {
    return null;
  }

  return (
    <Tips
      title={t("common.tips")}
      content={
        <Text color={open ? "success" : "danger"}>
          {open
            ? t("trading.rwa.marketHours")
            : t("trading.rwa.outsideMarketHours")}
        </Text>
      }
      trigger={
        <Box p={2} className="oui-flex oui-cursor-pointer oui-items-center">
          <Box
            width={4}
            height={4}
            r="full"
            className={cn(open ? "oui-bg-success" : "oui-bg-danger")}
          />
        </Box>
      }
    />
  );
};
