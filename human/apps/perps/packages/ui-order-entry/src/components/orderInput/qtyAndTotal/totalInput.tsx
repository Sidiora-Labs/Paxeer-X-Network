import { FC, memo, useEffect, useState } from "react";
import { useLocalStorage } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { inputFormatter } from "@paxeer-network/ui";
import { Decimal } from "@paxeer-network/utils";
import { InputType } from "../../../types";
import { CustomInput } from "../../customInput";
import { useOrderEntryContext } from "../../orderEntryContext";
import {
  OrderInputStackPosition,
  stackedOrderInputRoundedCn,
} from "./orderRowRadius";
import { OrderTotalType, TotalTypeSelect } from "./totalTypeSelect";

type TotalInputProps = {
  total?: string;
  stackPosition: OrderInputStackPosition;
};

/**
 * order size = order size (notional) = quantity * price
 * Initial margin = order size / leverage = quantity * price / leverage
 * Order size = Initial margin * leverage = quantity * price
 */
export const TotalInput: FC<TotalInputProps> = memo((props) => {
  const { t } = useTranslation();
  const { total } = props;

  const [margin, setMargin] = useState("");

  const {
    symbolInfo,
    onFocus,
    onBlur,
    getErrorMsg,
    setOrderValue,
    leverage = 1,
    currentFocusInput,
  } = useOrderEntryContext();

  const { quote } = symbolInfo;

  const [totalType, setTotalType] = useLocalStorage<OrderTotalType>(
    "paxeerNetwork_order_total_type",
    OrderTotalType.OrderSize,
  );

  useEffect(() => {
    if (total) {
      if (currentFocusInput !== InputType.MARGIN) {
        const margin = new Decimal(total).div(leverage).todp(2).toString();
        setMargin(margin);
      }
    } else {
      setMargin("");
    }
  }, [total, leverage, currentFocusInput]);

  const onMarginChange = (val: string) => {
    const total = val ? new Decimal(val).mul(leverage).toString() : "";
    setOrderValue("total", total);
    setMargin(val);
  };

  const prefix = <TotalTypeSelect value={totalType} onChange={setTotalType} />;

  if (totalType === OrderTotalType.InitialMargin) {
    return (
      <CustomInput
        id="order_margin_input"
        name="order_margin_input"
        label={`${t("orderEntry.initialMargin")}≈`}
        prefix={prefix}
        suffix={quote}
        error={getErrorMsg("total") ? "initial margin error" : ""}
        value={margin}
        onChange={onMarginChange}
        // national precision is 2
        formatters={[inputFormatter.dpFormatter(2)]}
        onFocus={onFocus(InputType.MARGIN)}
        onBlur={onBlur(InputType.MARGIN)}
        className={stackedOrderInputRoundedCn(props.stackPosition)}
        classNames={{
          root: "oui-orderEntry-marginInput",
          suffix: "oui-justify-end",
        }}
      />
    );
  }

  return (
    <CustomInput
      id="order_total_input"
      name="order_total_input"
      label={`${t("orderEntry.orderSize")}≈`}
      prefix={prefix}
      suffix={quote}
      error={getErrorMsg("total")}
      value={props.total}
      onChange={(val) => {
        setOrderValue("total", val);
      }}
      className={stackedOrderInputRoundedCn(props.stackPosition)}
      classNames={{
        root: "oui-orderEntry-totalInput",
        suffix: "oui-justify-end",
      }}
      // national precision is 2
      formatters={[inputFormatter.dpFormatter(2)]}
      onFocus={onFocus(InputType.TOTAL)}
      onBlur={onBlur(InputType.TOTAL)}
    />
  );
});

TotalInput.displayName = "TotalInput";
