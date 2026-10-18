import { memo } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { inputFormatter } from "@paxeer-network/ui";
import { InputType } from "../../../types";
import { CustomInput } from "../../customInput";
import { useOrderEntryContext } from "../../orderEntryContext";
import {
  OrderInputStackPosition,
  stackedOrderInputRoundedCn,
} from "../qtyAndTotal/orderRowRadius";

type TotalOrdersInputProps = {
  total_orders?: string;
  stackPosition: OrderInputStackPosition;
};

export const TotalOrdersInput = memo((props: TotalOrdersInputProps) => {
  const { t } = useTranslation();
  const { onFocus, onBlur, getErrorMsg, setOrderValue } =
    useOrderEntryContext();

  return (
    <CustomInput
      label={t("orderEntry.totalOrders")}
      placeholder="2-20"
      id="order_total_orders_input"
      className={stackedOrderInputRoundedCn(props.stackPosition)}
      value={props.total_orders}
      error={getErrorMsg("total_orders")}
      onChange={(val) => {
        setOrderValue("total_orders", val);
      }}
      overrideFormatters={[
        // inputFormatter.rangeFormatter({ min: 2, max: 20 }),
        inputFormatter.numberFormatter,
        inputFormatter.dpFormatter(0),
      ]}
      onFocus={onFocus(InputType.TOTAL_ORDERS)}
      onBlur={onBlur(InputType.TOTAL_ORDERS)}
      classNames={{
        root: "oui-orderEntry-scaledOrder-totalOrdersInput",
      }}
    />
  );
});
