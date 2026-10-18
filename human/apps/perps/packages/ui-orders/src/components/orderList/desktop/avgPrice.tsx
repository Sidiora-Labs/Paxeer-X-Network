import { memo } from "react";
import { useSymbolsInfo } from "@paxeer-network/hooks";
import { Text } from "@paxeer-network/ui";
import { Decimal } from "@paxeer-network/utils";

type AvgPriceProps = {
  symbol: string;
  value: string;
};

export const AvgPrice = memo((props: AvgPriceProps) => {
  const symbolsInfo = useSymbolsInfo();

  const info = symbolsInfo[props.symbol];

  if (!props.value) {
    return "--";
  }

  return (
    <Text.numeral
      padding={false}
      dp={info("quote_dp", 2)}
      rm={Decimal.ROUND_UP}
    >
      {props.value}
    </Text.numeral>
  );
});
