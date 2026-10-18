import { Text } from "@paxeer-network/ui";
import { Decimal } from "@paxeer-network/utils";
import { useSymbolContext } from "../../../provider/symbolContext";
import { PriceInput } from "./priceInput";
import { QuantityInput } from "./quantityInput";

export const renderQuantity = (value: number) => {
  // TODO(hooks): Convert this renderer to a React cell component before using hooks.
  const symbolInfo = useSymbolContext();
  return (
    <Text.numeral
      dp={symbolInfo.base_dp}
      rm={Decimal.ROUND_DOWN}
      padding={false}
      coloring
    >
      {value}
    </Text.numeral>
  );
};

export const renderQuantityInput = (value: number, record: any) => {
  return <QuantityInput value={record["position_qty"]} />;
};

export const renderPriceInput = (value: number) => {
  return <PriceInput />;
};
