import { DistributionType, PaxeerNetworkOrder } from "@paxeer-network/types";
import { cn } from "@paxeer-network/ui";
import { QuantityDistributionInput } from "./quantityDistributionInput";
import { ScaledPriceInput } from "./scaledPriceInput";
import { ScaledQuantityInput } from "./scaledQuantityInput";
import { SkewInput } from "./skewInput";
import { TotalOrdersInput } from "./totalOrdersInput";

type ScaledOrderInputProps = {
  values: Partial<PaxeerNetworkOrder>;
};

export const ScaledOrderInput = (props: ScaledOrderInputProps) => {
  const { values } = props;

  const showSkewInput = values.distribution_type === DistributionType.CUSTOM;

  return (
    <div className="oui-orderEntry-scaledOrderInput oui-space-y-1">
      <ScaledPriceInput
        start_price={values.start_price}
        end_price={values.end_price}
      />

      <div className="oui-scaledOrderInput-qtyAndOrders oui-space-y-1">
        <ScaledQuantityInput
          order_quantity={values.order_quantity}
          total={values.total}
          stackPosition="middle"
        />
        <TotalOrdersInput
          total_orders={values.total_orders}
          stackPosition="middle"
        />
      </div>

      <QuantityDistributionInput
        distribution_type={values.distribution_type}
        className={cn(!showSkewInput && "oui-rounded-b-xl")}
      />

      {showSkewInput && <SkewInput skew={values.skew} />}
    </div>
  );
};
