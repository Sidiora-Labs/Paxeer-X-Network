import { PaxeerNetworkOrder, OrderType } from "@paxeer-network/types";
import { PriceInput, PriceInputProps } from "./limit/priceInput";
import { QtyAndTotalInput } from "./qtyAndTotal/qtyAndTotalInput";
import { ScaledOrderInput } from "./scaledOrder";
import { TriggerPriceInput } from "./stop/triggerPriceInput";
import { TrailingStopInput } from "./trailingStop";

export type OrderInputProps = {
  values: Partial<PaxeerNetworkOrder>;
} & Omit<PriceInputProps, "order_price" | "order_type">;

export function OrderInput(props: OrderInputProps) {
  const { values } = props;

  const type = values.order_type;

  if (type === OrderType.SCALED) {
    return <ScaledOrderInput values={values} />;
  }

  if (type === OrderType.TRAILING_STOP) {
    return <TrailingStopInput values={values} />;
  }

  const showTriggerPrice =
    type === OrderType.STOP_LIMIT || type === OrderType.STOP_MARKET;

  const showPrice = type === OrderType.LIMIT || type === OrderType.STOP_LIMIT;

  const triggerPriceInput = showTriggerPrice && (
    <TriggerPriceInput trigger_price={values.trigger_price} />
  );

  const priceInput = showPrice && (
    <PriceInput
      order_type={values.order_type!}
      order_price={values.order_price}
      bbo={props.bbo}
      fillMiddleValue={props.fillMiddleValue}
      priceInputContainerWidth={props.priceInputContainerWidth}
    />
  );

  return (
    <div className={"oui-orderEntry-orderInput-container oui-space-y-1"}>
      {triggerPriceInput}
      {priceInput}
      <QtyAndTotalInput
        order_quantity={values.order_quantity}
        total={values.total}
        quantityStackPosition={showTriggerPrice || showPrice ? "middle" : "top"}
      />
    </div>
  );
}
