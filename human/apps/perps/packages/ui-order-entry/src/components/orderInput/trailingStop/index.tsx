import { PaxeerNetworkOrder } from "@paxeer-network/types";
import { QtyAndTotalInput } from "../qtyAndTotal/qtyAndTotalInput";
import { ActivePriceInput } from "./activePriceInput";
import { TrailingCallbackInput } from "./trailingCallbackInput";

type TrailingStopInputProps = {
  values: Partial<PaxeerNetworkOrder>;
};

export const TrailingStopInput = (props: TrailingStopInputProps) => {
  const { values } = props;

  return (
    <div className="oui-orderEntry-trailingStopInput oui-space-y-1">
      <ActivePriceInput activated_price={values.activated_price} />
      <TrailingCallbackInput
        callback_value={values.callback_value}
        callback_rate={values.callback_rate}
      />
      <QtyAndTotalInput
        order_quantity={values.order_quantity}
        total={values.total}
        quantityStackPosition="middle"
      />
    </div>
  );
};
