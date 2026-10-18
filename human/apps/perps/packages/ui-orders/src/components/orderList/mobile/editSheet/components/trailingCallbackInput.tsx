import { memo } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { cn, Input, inputFormatter, Text } from "@paxeer-network/ui";
import { useEditSheetContext } from "./editSheetContext";

type TrailingCallbackInputProps = {
  isCallbackValue?: boolean;
  callback_value?: number;
  callback_rate?: number;
};

export const TrailingCallbackInput = memo(
  (props: TrailingCallbackInputProps) => {
    const { isCallbackValue, callback_value, callback_rate } = props;
    const { t } = useTranslation();
    const { symbolInfo, setOrderValue, getErrorMsg } = useEditSheetContext();
    const { quote, quote_dp } = symbolInfo;

    const label = isCallbackValue
      ? t("orderEntry.trailingValue")
      : t("orderEntry.trailingRate");

    const field = isCallbackValue ? "callback_value" : "callback_rate";

    const suffix = isCallbackValue ? (
      <Text intensity={54} className="oui-px-3">
        {quote}
      </Text>
    ) : (
      "%"
    );

    const error = getErrorMsg(field);

    const formatters = [
      inputFormatter.numberFormatter,
      inputFormatter.decimalPointFormatter,
    ];

    return (
      <Input.tooltip
        prefix={
          <Text intensity={54} className="oui-px-3">
            {label}
          </Text>
        }
        suffix={suffix}
        color={error ? "danger" : undefined}
        align="right"
        fullWidth
        autoComplete="off"
        formatters={
          isCallbackValue
            ? [
                ...formatters,
                inputFormatter.currencyFormatter,
                inputFormatter.dpFormatter(quote_dp),
              ]
            : [...formatters, inputFormatter.dpFormatter(1)]
        }
        value={isCallbackValue ? callback_value : callback_rate}
        onValueChange={(val) => setOrderValue(field, val)}
        tooltip={error}
        tooltipProps={{
          content: {
            // mobile max width
            className: "oui-bg-base-5 oui-max-w-[360px]",
          },
          arrow: {
            className: "oui-fill-base-5",
          },
        }}
        classNames={{
          input: "oui-text-base-contrast",
          root: cn("oui-outline-line-12", error && "oui-outline-danger"),
        }}
      />
    );
  },
);

TrailingCallbackInput.displayName = "TrailingCallbackInput";
