import {
  isCurrentlyTrading,
  useGetRwaSymbolOpenStatus,
} from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { Text, Tooltip, Box } from "@paxeer-network/ui";

export type RwaDotTooltipProps = {
  record: any;
};

export const RwaDotTooltip = ({ record }: RwaDotTooltipProps) => {
  const { t } = useTranslation();
  const { open: computedOpen } = useGetRwaSymbolOpenStatus(record.symbol);
  const open =
    computedOpen ??
    isCurrentlyTrading(
      record.rwaNextClose,
      record.rwaStatus,
      undefined,
      record.rwaNextOpen,
    );

  if (!record.isRwa) {
    return null;
  }

  return (
    <Tooltip
      className="oui-pointer-events-none"
      disableHoverableContent
      content={
        <Text color={open ? "success" : "danger"}>
          {open
            ? t("trading.rwa.marketHours")
            : t("trading.rwa.outsideMarketHours")}
        </Text>
      }
    >
      <Box
        as="span"
        py={2}
        px={1}
        className="oui-inline-flex oui-shrink-0 oui-items-center"
      >
        <Box
          as="span"
          width={4}
          height={4}
          r="full"
          className={open ? "oui-bg-success" : "oui-bg-danger"}
        />
      </Box>
    </Tooltip>
  );
};
