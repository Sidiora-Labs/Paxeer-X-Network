import { memo } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { inputFormatter } from "@paxeer-network/ui";
import { InputType } from "../../../types";
import { CustomInput } from "../../customInput";
import { useOrderEntryContext } from "../../orderEntryContext";

type ActivePriceInputProps = {
  activated_price?: string;
};

export const ActivePriceInput = memo<ActivePriceInputProps>((props) => {
  const { t } = useTranslation();
  const {
    symbolInfo,
    onFocus,
    onBlur,
    getErrorMsg,
    setOrderValue,
    activatedPriceInputRef,
  } = useOrderEntryContext();

  return (
    <div className="oui-orderEntry-activatedPriceInput-container oui-group">
      <CustomInput
        ref={activatedPriceInputRef}
        id="order_activated_price_input"
        name="order_activated_price_input"
        label={t("common.triggerPrice")}
        placeholder={`0 (${t("common.optional")})`}
        suffix={symbolInfo.quote}
        error={getErrorMsg("activated_price")}
        value={props.activated_price}
        className="oui-orderEntry-activatedPriceInput"
        onChange={(val: string) => {
          setOrderValue("activated_price", val);
        }}
        formatters={[inputFormatter.dpFormatter(symbolInfo.quote_dp)]}
        onFocus={onFocus(InputType.ACTIVATED_PRICE)}
        onBlur={onBlur(InputType.ACTIVATED_PRICE)}
      />
    </div>
  );
});

ActivePriceInput.displayName = "ActivePriceInput";
