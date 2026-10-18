import { FC, ReactNode } from "react";
import { cn, Flex, Text } from "@paxeer-network/ui";

interface SwapCoinProps {
  className?: string;
  sourceSymbol?: string;
  targetSymbol?: string;
  indexPrice: number | string;
  precision?: number;
  suffix?: ReactNode;
}

export const SwapCoin: FC<SwapCoinProps> = (props) => {
  const {
    sourceSymbol,
    targetSymbol,
    indexPrice,
    precision = 6,
    suffix,
  } = props;

  return (
    <Flex
      itemAlign="center"
      gap={1}
      className={cn(props.className, "oui-text-2xs oui-text-base-contrast-36")}
    >
      <Text intensity={80}>1</Text>
      <span>{sourceSymbol}</span>
      <span>=</span>

      <Text.numeral intensity={80} dp={precision} padding={false}>
        {indexPrice}
      </Text.numeral>

      <span>{targetSymbol}</span>
      {suffix}
    </Flex>
  );
};
