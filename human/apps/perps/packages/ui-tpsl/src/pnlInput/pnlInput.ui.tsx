import { useMemo, useState } from "react";
import {
  CaretDownIcon,
  cn,
  Input,
  MenuItem,
  SimpleDropdownMenu,
} from "@paxeer-network/ui";
import { inputFormatter, Text } from "@paxeer-network/ui";
import { PNLInputState, PnLMode } from "./useBuilder.script";

export type PNLInputProps = PNLInputState & {
  testId?: string;
  quote: string;
};

export const PNLInput = (props: PNLInputProps) => {
  const {
    mode,
    modes,
    modeLabelMap,
    onModeChange,
    onValueChange,
    quote,
    quote_dp,
    value,
    pnl,
    disabled,
  } = props;

  const [isInputFocused, setIsInputFocused] = useState(false);

  const color = useMemo(() => {
    const num = Number(pnl);

    if (isNaN(num) || num === 0) return "";

    if (num > 0) return "oui-text-trade-profit";
    if (num < 0) return "oui-text-trade-loss";
  }, [pnl]);

  /** Use dedicated map so input prefix is decoupled from dropdown labels. */
  const prefixLabel = String(modeLabelMap[mode as PnLMode] ?? mode ?? "");
  /** Keep placeholder behavior while avoiding effect-driven state sync. */
  const placeholder = isInputFocused
    ? ""
    : mode === PnLMode.PERCENTAGE || mode === PnLMode.PERCENTAGE_FROM_MARK
      ? "%"
      : quote;

  return (
    <Input
      prefix={prefixLabel}
      size={{
        initial: "lg",
        lg: "md",
      }}
      placeholder={placeholder}
      align={"right"}
      value={value}
      disabled={disabled}
      data-testid={props.testId}
      autoComplete={"off"}
      onValueChange={onValueChange}
      formatters={[
        // inputFormatter.numberFormatter,
        props.formatter({ dp: quote_dp, mode }),
        inputFormatter.currencyFormatter,
        inputFormatter.decimalPointFormatter,
      ]}
      // className={color}
      classNames={{
        input: cn("oui-text-2xs", color),
        prefix: "oui-text-base-contrast-54 oui-text-2xs",
        root: cn(
          "oui-outline-line-12 focus-within:oui-outline-primary-light",
          disabled && "oui-bg-base-6 oui-opacity-50 oui-cursor-not-allowed",
        ),
      }}
      onFocus={() => {
        setIsInputFocused(true);
        props.setFocus(true);
      }}
      onBlur={() => {
        setIsInputFocused(false);
        props.setFocus(false);
      }}
      // value={props.value}
      suffix={
        <>
          {(mode === PnLMode.PERCENTAGE ||
            mode === PnLMode.PERCENTAGE_FROM_MARK) &&
            !!value && (
              <Text
                size={"2xs"}
                color="inherit"
                className={cn("oui-ms-[2px]", color)}
              >
                %
              </Text>
            )}
          <PNLMenus
            disabled={disabled}
            mode={mode}
            modes={modes}
            onModeChange={(item) => onModeChange(item.value as PnLMode)}
          />
        </>
      }
    />
  );
};

const PNLMenus = (props: {
  disabled?: boolean;
  mode?: string;
  modes: MenuItem[];
  onModeChange: (value: MenuItem) => void;
}) => {
  if (props.disabled) {
    return (
      <button
        type="button"
        className="oui-p-2 oui-text-base-contrast-54 oui-cursor-not-allowed"
        disabled
      >
        <CaretDownIcon size={12} color="inherit" />
      </button>
    );
  }

  return (
    <SimpleDropdownMenu
      currentValue={props.mode}
      menu={props.modes}
      align={"end"}
      size={"xs"}
      className={"oui-min-w-[120px]"}
      onSelect={(item) => {
        if (!props.disabled) {
          props.onModeChange(item as MenuItem);
        }
      }}
    >
      <button type="button" className="oui-p-2">
        <CaretDownIcon size={12} color="white" />
      </button>
    </SimpleDropdownMenu>
  );
};
