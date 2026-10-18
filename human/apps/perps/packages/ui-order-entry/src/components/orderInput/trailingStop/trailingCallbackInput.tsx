import { FC, memo, useCallback, useRef } from "react";
import { useLocalStorage } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { TrailingCallbackType } from "@paxeer-network/types";
import { inputFormatter } from "@paxeer-network/ui";
import { InputType } from "../../../types";
import { CustomInput } from "../../customInput";
import { useOrderEntryContext } from "../../orderEntryContext";
import { CallbackRatePercentages } from "./callbackRatePercentages";
import { TrailingCallbackSelect } from "./trailingCallbackSelect";

type TrailingCallbackInputProps = {
  callback_value?: string;
  callback_rate?: string;
};

export const TrailingCallbackInput: FC<TrailingCallbackInputProps> = memo(
  (props) => {
    const { callback_value, callback_rate } = props;
    const { t } = useTranslation();
    const {
      symbolInfo,
      onFocus,
      onBlur,
      getErrorMsg,
      setOrderValue,
      setOrderValues,
    } = useOrderEntryContext();
    const { quote, quote_dp } = symbolInfo;
    // const [open, setOpen] = useState(false);

    const lastCallbackValueRef = useRef<string>();
    const lastCallbackRateRef = useRef<string>();

    const [callbackType, setCallbackType] = useLocalStorage(
      "paxeerNetwork_order_trailing_callback_type",
      TrailingCallbackType.VALUE,
    );

    const onCallbackTypeChange = useCallback(
      (type: TrailingCallbackType) => {
        setCallbackType(type);
        // when switch to callback rate, save the last callback value
        if (type === TrailingCallbackType.RATE) {
          lastCallbackValueRef.current = callback_value;
          setOrderValues({
            callback_value: "",
            callback_rate: lastCallbackRateRef.current,
          });
          // when switch to callback value, save the last callback rate
        } else {
          lastCallbackRateRef.current = callback_rate;
          setOrderValues({
            callback_value: lastCallbackValueRef.current,
            callback_rate: "",
          });
        }
      },
      [callback_value, callback_rate],
    );

    const suffix = (
      <TrailingCallbackSelect
        quote={quote}
        value={callbackType}
        onValueChange={onCallbackTypeChange}
      />
    );

    // don't need to convert trailing rate to estimated value
    // const estimatedValue = useMemo(() => {
    //   if (callbackType === TrailingCallbackType.RATE) {
    //     return callback_rate
    //   }
    // }, [callback_rate, callbackType]);

    // const tooltipContent = estimatedValue
    //   ? `≈ ${estimatedValue} ${quote}`
    //   : undefined;

    // const openTooltip = useCallback(() => {
    //   setOpen(true);
    // }, []);

    // const closeTooltip = useCallback(() => {
    //   setOpen(false);
    // }, []);

    if (callbackType === TrailingCallbackType.RATE) {
      return (
        // <Tooltip
        //   content={tooltipContent}
        //   open={open && !!tooltipContent}
        //   sideOffset={-25}
        //   className="!oui-p-3"
        // >
        <div className="oui-orderEntry-trailingCallback oui-relative">
          <CustomInput
            id="order_callback_rate_input"
            name="order_callback_rate_input"
            label={t("orderEntry.trailingRate")}
            suffix={suffix}
            error={getErrorMsg("callback_rate")}
            value={callback_rate}
            className="oui-trailingCallback-rateInput"
            onChange={(val: string) => {
              setOrderValue("callback_rate", val);
            }}
            formatters={[inputFormatter.dpFormatter(1)]}
            onFocus={(e) => {
              onFocus(InputType.CALLBACK_RATE)(e);
              // openTooltip();
            }}
            onBlur={(e) => {
              onBlur(InputType.CALLBACK_RATE)(e);
              // closeTooltip();
            }}
            classNames={{
              root: "oui-h-[68px]",
              input: "oui-mb-5",
              prefix: "!oui-top-1",
            }}
          />
          <CallbackRatePercentages
            className="oui-trailingCallback-percentages oui-absolute oui-bottom-1 oui-start-2"
            callback_rate={callback_rate}
            // onClick={openTooltip}
          />
        </div>
        // </Tooltip>
      );
    }

    return (
      <CustomInput
        id="order_callback_value_input"
        name="order_callback_value_input"
        label={t("orderEntry.trailingValue")}
        suffix={suffix}
        error={getErrorMsg("callback_value")}
        value={callback_value}
        className="oui-trailingCallback-valueInput"
        onChange={(val: string) => {
          setOrderValue("callback_value", val);
        }}
        formatters={[inputFormatter.dpFormatter(quote_dp)]}
        onFocus={onFocus(InputType.CALLBACK_VALUE)}
        onBlur={onBlur(InputType.CALLBACK_VALUE)}
        classNames={{
          input: "!oui-mb-[6px]",
          prefix: "!oui-top-1",
        }}
      />
    );
  },
);

TrailingCallbackInput.displayName = "TrailingCallbackInput";
