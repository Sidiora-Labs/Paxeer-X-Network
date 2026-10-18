import { FC, useMemo } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { cn, Flex, Text } from "@paxeer-network/ui";
import { Decimal, formatWithPrecision } from "@paxeer-network/utils";

interface ConvertRateProps {
  className?: string;
  sourceSymbol?: string;
  targetSymbol?: string;
  swapPrice: number | string;
  precision?: number;
  swapPriceInUSD?: number | string;
}

export const ConvertRate: FC<ConvertRateProps> = (props) => {
  const {
    sourceSymbol,
    targetSymbol,
    swapPrice,
    precision = 6,
    swapPriceInUSD,
  } = props;

  const { t } = useTranslation();

  const formatSwapPrice = useMemo(() => {
    return formatWithPrecision(swapPrice, precision);
  }, [swapPrice, precision]);

  const suffix = swapPriceInUSD ? (
    <div>
      ($<Text>{swapPriceInUSD}</Text>)
    </div>
  ) : undefined;

  return (
    <Flex width={"100%"} itemAlign="center" justify="between">
      <Text size="2xs" intensity={36}>
        {t("transfer.deposit.convertRate")}
      </Text>
      <Flex
        itemAlign="center"
        gap={1}
        className={cn(
          props.className,
          "oui-text-2xs oui-text-base-contrast-36",
        )}
      >
        <Text intensity={80}>1</Text>
        <span>{sourceSymbol}</span>
        <span>=</span>

        <Text intensity={80}>{formatSwapPrice}</Text>

        <span>{targetSymbol}</span>
        {suffix}
      </Flex>
    </Flex>
  );
};
