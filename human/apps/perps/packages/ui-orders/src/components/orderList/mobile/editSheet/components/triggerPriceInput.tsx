import { memo } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { cn, Input, inputFormatter, Text } from "@paxeer-network/ui";
import { useEditSheetContext } from "./editSheetContext";

type TriggerPriceInputProps = {
  trigger_price?: string | number;
  disabled?: boolean;
};

export const TriggerPriceInput = memo((props: TriggerPriceInputProps) => {
  const { t } = useTranslation();

  const { symbolInfo, setOrderValue, getErrorMsg } = useEditSheetContext();
  const { quote, quote_dp } = symbolInfo;

  const error = getErrorMsg("trigger_price");

  return (
    <Input.tooltip
      prefix={
        <Text intensity={54} className="oui-px-3">
          {t("common.triggerPrice")}
        </Text>
      }
      suffix={
        <Text intensity={54} className="oui-px-3">
          {quote}
        </Text>
      }
      color={error ? "danger" : undefined}
      align="right"
      fullWidth
      autoComplete="off"
      formatters={[
        inputFormatter.numberFormatter,
        inputFormatter.dpFormatter(quote_dp),
      ]}
      value={props.trigger_price}
      onValueChange={(val) => setOrderValue("trigger_price", val)}
      disabled={props.disabled}
      tooltip={error}
      tooltipProps={{
        content: {
          className: "oui-bg-base-6 oui-text-base-contrast-80",
        },
        arrow: {
          className: "oui-fill-base-6",
        },
      }}
      classNames={{
        input: "oui-text-base-contrast oui-w-full",
        root: cn("oui-outline-line-12", error && "oui-outline-danger"),
      }}
    />
  );
});

TriggerPriceInput.displayName = "TriggerPriceInput";
