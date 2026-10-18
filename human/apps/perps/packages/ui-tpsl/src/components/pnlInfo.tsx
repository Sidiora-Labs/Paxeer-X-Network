import { useMemo } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { cn, Flex, Text } from "@paxeer-network/ui";
import { Decimal, checkIsNaN } from "@paxeer-network/utils";

type Props = {
  tp_pnl?: string | number;
  sl_pnl?: string | number;
  className?: string;
};

export const PnlInfo = (props: Props) => {
  const { t } = useTranslation();
  const { tp_pnl, sl_pnl } = props;
  const riskRatio = useMemo(() => {
    const defaultNode = <Text className="oui-text-base-contrast-36">-- x</Text>;
    if (tp_pnl === undefined || sl_pnl === undefined) {
      return defaultNode;
    }
    if (checkIsNaN(tp_pnl) || checkIsNaN(sl_pnl)) {
      return defaultNode;
    }
    const tpDecimal = new Decimal(tp_pnl);
    const slDecimal = new Decimal(sl_pnl);
    if (slDecimal.isZero() || tpDecimal.isZero()) {
      return defaultNode;
    }
    const ratio = tpDecimal.div(slDecimal).abs().toNumber().toFixed(2);
    return (
      <Flex gap={1} itemAlign={"center"} className="oui-text-base-contrast-80">
        <Text>{ratio}</Text>
        <Text className="oui-text-base-contrast-36">x</Text>
      </Flex>
    );
    return defaultNode;
  }, [tp_pnl, sl_pnl]);
  return (
    <Flex
      direction={"column"}
      itemAlign={"start"}
      className={cn(
        "oui-w-full oui-gap-1 oui-text-2xs oui-text-base-contrast-36",
        props.className,
      )}
    >
      <Flex justify={"between"} className="oui-w-full">
        <Text size="2xs">{t("tpsl.totalEstTpPnl")}</Text>
        <Text.numeral
          suffix={
            <Text className="oui-text-base-contrast-36 oui-ms-1">USDC</Text>
          }
          rule="price"
          coloring
          visible={true}
          size="2xs"
          dp={2}
        >
          {tp_pnl ? Number(tp_pnl) : "--"}
        </Text.numeral>
      </Flex>
      <Flex justify={"between"} className="oui-w-full">
        <Text size="2xs">{t("tpsl.totalEstSlPnl")}</Text>
        {sl_pnl ? (
          <Text.numeral
            suffix={
              <Text className="oui-text-base-contrast-36 oui-ms-1">USDC</Text>
            }
            coloring
            visible={true}
            size="2xs"
            dp={2}
          >
            {Number(sl_pnl)}
          </Text.numeral>
        ) : (
          <Text size="2xs">-- USDC</Text>
        )}
      </Flex>
      <Flex justify={"between"} className="oui-w-full">
        <Text size="2xs">{t("tpsl.riskRewardRatio")}</Text>
        <Text className="oui-text-base-contrast-80" size="2xs">
          {riskRatio}
        </Text>
      </Flex>
    </Flex>
  );
};
