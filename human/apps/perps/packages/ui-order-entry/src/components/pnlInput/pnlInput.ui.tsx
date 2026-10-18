import { useEffect, useMemo, useState } from "react";
import {
  CaretDownIcon,
  cn,
  Input,
  MenuItem,
  Text,
  SimpleDropdownMenu,
} from "@paxeer-network/ui";
import { inputFormatter } from "@paxeer-network/ui";
import { PNLInputState, PnLMode } from "./useBuilder.script";

export type PNLInputProps = PNLInputState & {
  testIds?: {
    input?: string;
    dropDown?: string;
  };
  quote: string;
  type: "TP" | "SL";
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
    type,
    tips,
    onFocus,
    onBlur,
    setFocus,
  } = props;

  const [prefix, setPrefix] = useState<string>(mode);
  const isPercentageMode =
    mode === PnLMode.PERCENTAGE || mode === PnLMode.PERCENTAGE_FROM_MARK;

  const [placeholder, setPlaceholder] = useState<string>(
    isPercentageMode ? "%" : quote,
  );

  useEffect(() => {
    setPrefix(mode);
    setPlaceholder(isPercentageMode ? "%" : quote);
  }, [mode, isPercentageMode, quote]);

  useEffect(() => {
    setPrefix(!!value ? "" : mode);
  }, [value]);

  const id = useMemo(
    () => `${type.toLowerCase()}_${mode.toLowerCase()}`,
    [type, mode],
  );

  /** Resolve label for prefix state; `prefix` can be "" while focused. */
  const prefixLabel =
    prefix === ""
      ? ""
      : String(
          (modeLabelMap as Record<string, string>)[prefix] ?? prefix ?? "",
        );

  return (
    <Input.tooltip
      prefix={prefixLabel}
      size={"md"}
      placeholder={placeholder}
      id={id}
      align={"right"}
      value={value}
      tooltip={tips}
      tooltipProps={{
        content: {
          side: props.type === "TP" ? "top" : "bottom",
        },
      }}
      data-testid={props.testIds?.input}
      autoComplete={"off"}
      onValueChange={onValueChange}
      formatters={[
        inputFormatter.numberFormatter,
        props.formatter({ dp: quote_dp, mode, type }),
        inputFormatter.currencyFormatter,
        // inputFormatter.identifierFormatter(),
      ]}
      classNames={{
        root: cn(
          "oui-orderEntry-pnlInput-container",
          type === "TP" ? "oui-text-trade-profit" : "oui-text-trade-loss",
        ),
        additional: "oui-text-base-contrast-54",
        input: "oui-orderEntry-pnlInput oui-text-inherit",
      }}
      onFocus={() => {
        setPrefix("");
        setPlaceholder("");
        setFocus(true);
        onFocus();
      }}
      onBlur={() => {
        setPrefix(!!value ? "" : mode);
        setPlaceholder(isPercentageMode ? "%" : quote);
        onBlur();
      }}
      suffix={
        <>
          {isPercentageMode && !!value && (
            <Text size={"2xs"} color="inherit" className="oui-ms-[2px]">
              %
            </Text>
          )}
          <PNLMenus
            mode={mode}
            modes={modes}
            onModeChange={(item) => onModeChange(item.value as PnLMode)}
            testId={props.testIds?.dropDown}
          />
        </>
      }
    />
  );
};

const PNLMenus = (props: {
  mode?: string;
  modes: MenuItem[];
  onModeChange: (value: MenuItem) => void;
  testId?: string;
}) => {
  return (
    <SimpleDropdownMenu
      currentValue={props.mode}
      menu={props.modes}
      align={"end"}
      size={"xs"}
      className={"oui-pnlInput-menu oui-min-w-[80px]"}
      onCloseAutoFocus={(event) => event.preventDefault()}
      onSelect={(item) => props.onModeChange(item as MenuItem)}
    >
      <button
        className={"oui-pnlInput-menuTrigger-btn oui-p-2"}
        data-testid={props.testId}
      >
        <CaretDownIcon size={12} color={"white"} />
      </button>
    </SimpleDropdownMenu>
  );
};
