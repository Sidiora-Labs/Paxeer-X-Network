import { useState } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { cn, Input, inputFormatter } from "@paxeer-network/ui";

export const PriceInput: React.FC<{
  type: string;
  label?: string;
  value?: string | number;
  error?: string;
  onValueChange: (value: string) => void;
  quote_dp: number;
  disabled?: boolean;
  classNames?: {
    root?: string;
    input?: string;
    prefix?: string;
  };
}> = (props) => {
  const [placeholder, setPlaceholder] = useState<string>("USDC");
  const { t } = useTranslation();

  return (
    <Input.tooltip
      data-testid={`oui-testid-tpsl-popUp-${props.type.toLowerCase()}-input`}
      prefix={props.label ?? t("common.markPrice")}
      size={{ initial: "lg", lg: "md" }}
      tooltip={props.error}
      placeholder={placeholder}
      disabled={props.disabled}
      align={"right"}
      autoComplete={"off"}
      value={props.value}
      color={props.error ? "danger" : undefined}
      classNames={{
        input: cn(
          "oui-text-2xs placeholder:oui-text-2xs",
          props.classNames?.input,
        ),
        prefix: cn(
          "oui-text-base-contrast-54 oui-text-2xs",
          props.classNames?.prefix,
        ),
        root: cn("oui-w-full", props.classNames?.root),
      }}
      onValueChange={props.onValueChange}
      onFocus={() => {
        setPlaceholder("");
      }}
      onBlur={() => {
        setPlaceholder("USDC");
      }}
      formatters={[
        inputFormatter.numberFormatter,
        inputFormatter.dpFormatter(props.quote_dp),
        inputFormatter.currencyFormatter,
        inputFormatter.decimalPointFormatter,
      ]}
    />
  );
};
