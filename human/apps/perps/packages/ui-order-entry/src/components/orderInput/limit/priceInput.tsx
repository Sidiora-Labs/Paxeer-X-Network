import { FC } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { OrderType } from "@paxeer-network/types";
import { cn, inputFormatter } from "@paxeer-network/ui";
import { OrderEntryScriptReturn } from "../../../orderEntry.script";
import { InputType } from "../../../types";
import { BBOStatus } from "../../../utils";
import { CustomInput } from "../../customInput";
import { useOrderEntryContext } from "../../orderEntryContext";
import { BBOOrderTypeSelect } from "./bboOrderTypeSelect";
import { LimitPriceSuffix } from "./limitPriceSuffix";

export type PriceInputProps = {
  order_type: OrderType;
  order_price?: string;
  bbo: Pick<
    OrderEntryScriptReturn,
    "bboStatus" | "bboType" | "onBBOChange" | "toggleBBO"
  >;
  priceInputContainerWidth?: number;
  fillMiddleValue: OrderEntryScriptReturn["fillMiddleValue"];
};

// TODO: memo component
export const PriceInput: FC<PriceInputProps> = (props) => {
  const { bbo } = props;
  const { t } = useTranslation();
  const {
    symbolInfo,
    onFocus,
    onBlur,
    getErrorMsg,
    setOrderValue,
    priceInputRef,
    priceInputContainerRef,
  } = useOrderEntryContext();

  const { quote, quote_dp } = symbolInfo;

  const readOnly = bbo.bboStatus === BBOStatus.ON;

  const suffix =
    props.order_type === OrderType.LIMIT ? (
      <LimitPriceSuffix
        quote={quote}
        bbo={bbo}
        fillMiddleValue={props.fillMiddleValue}
      />
    ) : (
      quote
    );

  return (
    <div
      ref={priceInputContainerRef}
      className="oui-orderEntry-priceInput-container oui-group oui-relative oui-w-full"
    >
      <CustomInput
        id="order_price_input"
        name="order_price_input"
        label={t("common.price")}
        suffix={suffix}
        value={props.order_price}
        className="oui-orderEntry-priceInput"
        onChange={(e) => {
          setOrderValue("order_price", e);
        }}
        error={getErrorMsg("order_price")}
        formatters={[inputFormatter.dpFormatter(quote_dp)]}
        onFocus={onFocus(InputType.PRICE)}
        onBlur={onBlur(InputType.PRICE)}
        readonly={readOnly}
        ref={priceInputRef}
        classNames={{
          root: cn(readOnly && "focus-within:oui-outline-transparent "),
          input: cn(readOnly && "oui-cursor-auto"),
        }}
      />
      {bbo.bboStatus === BBOStatus.ON && (
        <div
          className={cn(
            "oui-orderEntry-bboSelect",
            "oui-absolute oui-bottom-1 oui-start-0",
          )}
        >
          <BBOOrderTypeSelect
            value={bbo.bboType}
            onChange={bbo.onBBOChange}
            contentStyle={{
              width: props.priceInputContainerWidth,
            }}
          />
        </div>
      )}
    </div>
  );
};
