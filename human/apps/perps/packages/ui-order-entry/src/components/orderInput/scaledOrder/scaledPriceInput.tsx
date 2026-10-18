import { memo } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { inputFormatter } from "@paxeer-network/ui";
import { InputType } from "../../../types";
import { CustomInput } from "../../customInput";
import { useOrderEntryContext } from "../../orderEntryContext";

type ScaledPriceInputProps = {
  start_price?: string;
  end_price?: string;
};

export const ScaledPriceInput = memo((props: ScaledPriceInputProps) => {
  const { t } = useTranslation();

  const { symbolInfo, onFocus, onBlur, getErrorMsg, setOrderValue } =
    useOrderEntryContext();

  const { quote, quote_dp } = symbolInfo;

  return (
    <>
      <CustomInput
        label={t("orderEntry.startPrice")}
        suffix={quote}
        id="order_start_price_input"
        className="oui-orderEntry-scaledOrder-startPriceInput"
        value={props.start_price}
        error={getErrorMsg("start_price")}
        onChange={(e) => {
          setOrderValue("start_price", e);
        }}
        formatters={[inputFormatter.dpFormatter(quote_dp)]}
        onFocus={onFocus(InputType.START_PRICE)}
        onBlur={onBlur(InputType.START_PRICE)}
        classNames={{
          root: "oui-rounded-t-xl",
        }}
      />
      <CustomInput
        label={t("orderEntry.endPrice")}
        suffix={quote}
        id="order_end_price_input"
        className="oui-orderEntry-scaledOrder-endPriceInput"
        value={props.end_price}
        error={getErrorMsg("end_price")}
        onChange={(val) => {
          setOrderValue("end_price", val);
        }}
        formatters={[inputFormatter.dpFormatter(quote_dp)]}
        onFocus={onFocus(InputType.END_PRICE)}
        onBlur={onBlur(InputType.END_PRICE)}
      />
    </>
  );
});

ScaledPriceInput.displayName = "ScaledPriceInput";
