import { FC, memo } from "react";
import { OrderInputStackPosition } from "./orderRowRadius";
import { QuantityInput } from "./quantityInput";
import { TotalInput } from "./totalInput";

type QtyAndTotalInputProps = {
  order_quantity?: string;
  total?: string;
  quantityStackPosition: OrderInputStackPosition;
};

export const QtyAndTotalInput: FC<QtyAndTotalInputProps> = memo((props) => {
  return (
    <div className="oui-orderEntry-qtyAndTotal oui-space-y-1">
      <QuantityInput
        order_quantity={props.order_quantity}
        stackPosition={props.quantityStackPosition}
      />
      <TotalInput total={props.total} stackPosition="bottom" />
    </div>
  );
});

QtyAndTotalInput.displayName = "QtyAndTotalInput";
