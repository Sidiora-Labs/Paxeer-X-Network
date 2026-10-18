import { useTranslation } from "@paxeer-network/i18n";
import { Flex, cn, modal, Text } from "@paxeer-network/ui";
import { OrderEntryScriptReturn } from "../../../orderEntry.script";
import { BBOStatus } from "../../../utils";

type LimitPriceSuffixProps = {
  quote: string;
  bbo: Pick<
    OrderEntryScriptReturn,
    "bboStatus" | "bboType" | "onBBOChange" | "toggleBBO"
  >;
  fillMiddleValue: OrderEntryScriptReturn["fillMiddleValue"];
};

export const LimitPriceSuffix = (props: LimitPriceSuffixProps) => {
  const { quote, bbo, fillMiddleValue } = props;
  const { t } = useTranslation();

  return (
    <Flex
      direction="column"
      itemAlign="end"
      className={cn("oui-orderEntry-limitPrice-suffix", "oui-text-2xs")}
    >
      {quote}
      <Flex justify={"end"} itemAlign="center" gap={2}>
        <Flex
          px={3}
          height={20}
          justify="center"
          itemAlign="center"
          r="base"
          className={cn(
            "oui-orderEntry-bboToggle",
            "oui-mt-[2px] oui-cursor-pointer oui-select-none oui-border",
            bbo.bboStatus === BBOStatus.ON
              ? "oui-border-primary"
              : "oui-border-line-12",
            bbo.bboStatus === BBOStatus.DISABLED && "oui-cursor-not-allowed",
          )}
          onClick={() => {
            if (bbo.bboStatus === BBOStatus.DISABLED) {
              modal.dialog({
                title: t("common.tips"),
                size: "xs",
                content: (
                  <Text intensity={54}>
                    {t("orderEntry.bbo.disabled.tips")}
                  </Text>
                ),
              });
            } else {
              bbo.toggleBBO();
            }
          }}
        >
          <Text
            className={cn(
              "oui-orderEntry-bboToggle-label",
              bbo.bboStatus === BBOStatus.ON && "oui-text-primary",
              bbo.bboStatus === BBOStatus.OFF && "oui-text-base-contrast-54",
              bbo.bboStatus === BBOStatus.DISABLED &&
                "oui-text-base-contrast-20",
            )}
          >
            {t("orderEntry.bbo")}
          </Text>
        </Flex>
        <Text
          className={cn(
            "oui-orderEntry-midPrice-btn",
            "oui-select-none",
            "oui-cursor-pointer oui-text-primary",
          )}
          onClick={fillMiddleValue}
        >
          Mid
        </Text>
      </Flex>
    </Flex>
  );
};
