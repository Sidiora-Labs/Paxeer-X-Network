import React, { useEffect, useMemo, useState } from "react";
import { ERROR_MSG_CODES, OrderValidationResult } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { useOrderEntryFormErrorMsg } from "@paxeer-network/react-app";
import { PaxeerNetworkOrder, OrderType, PositionType } from "@paxeer-network/types";
import {
  cn,
  Flex,
  Text,
  Input,
  inputFormatter,
  Switch,
  SettingFillIcon,
  useScreen,
  DotStatus,
} from "@paxeer-network/ui";
import { Grid } from "@paxeer-network/ui";
import { TPSLPositionTypeWidget } from "@paxeer-network/ui-tpsl";
import { useOrderEntryContext } from "./orderEntryContext";
import { PnlInputWidget } from "./pnlInput/pnlInput.widget";
import { usePnlInputContext } from "./pnlInput/pnlInputContext";
import { PnlInputProvider } from "./pnlInput/pnlInputProvider";
import { PNL_Values } from "./pnlInput/useBuilder.script";
import { ReduceOnlySwitch } from "./reduceOnlySwitch";

type OrderValueKeys = keyof PaxeerNetworkOrder;

type Est_Values = PNL_Values & {
  trigger_price?: string;
};

type TPSL_Values = {
  tp: Est_Values;
  sl: Est_Values;
  position_type: PositionType;
};

export const OrderTPSL = (props: {
  // onCancelTPSL: () => void;
  // onEnableTP_SL: () => void;
  switchState: boolean;
  onSwitchChanged: (state: boolean) => void;
  onChange: (key: OrderValueKeys, value: any) => void;
  values: TPSL_Values;
  orderType: OrderType;
  errors: OrderValidationResult | null;
  quote_dp: number | undefined;
  showTPSLAdvanced: () => void;
  setOrderValue: (key: string, value: any) => void;
  reduceOnlyChecked?: boolean;
  onReduceOnlyChange?: (checked: boolean) => void;
}) => {
  const tpslFormRef = React.useRef<HTMLDivElement>(null);
  const { t } = useTranslation();
  const { isMobile } = useScreen();

  useEffect(() => {
    if (
      props.orderType !== OrderType.LIMIT &&
      props.orderType !== OrderType.MARKET
    ) {
      props.onSwitchChanged(false);
    }
  }, [props.orderType]);

  if (
    props.orderType !== OrderType.LIMIT &&
    props.orderType !== OrderType.MARKET
  ) {
    return null;
  }

  const isSlPriceWarning =
    props.errors?.["sl_trigger_price"]?.["type"] ===
    ERROR_MSG_CODES.SL_PRICE_WARNING;

  return (
    <div className="oui-orderEntry-tpsl">
      <Flex
        itemAlign={"center"}
        justify={"between"}
        className="oui-orderEntry-tpsl-header"
      >
        <Flex itemAlign={"center"} gapX={1}>
          <Switch
            id={"order_entry_tpsl"}
            className="oui-h-[14px]"
            checked={props.switchState}
            disabled={
              props.orderType !== OrderType.LIMIT &&
              props.orderType !== OrderType.MARKET
            }
            onCheckedChange={(checked) => {
              props.onSwitchChanged(checked);
            }}
          />
          <label htmlFor={"order_entry_tpsl"} className={"oui-text-xs"}>
            {t("common.tpsl")}
          </label>
          {/* <ExclamationFillIcon
            color="white"
            // opacity={0.36}
            size={14}
            opacity={1}
            className="oui-cursor-pointer oui-text-base-contrast-36 hover:oui-text-base-contrast-80"
            onClick={() => {
              modal.dialog({
                title: t("common.tips"),
                size: "xs",
                content: (
                  <Text intensity={54}>{t("orderEntry.tpsl.tips")}</Text>
                ),
              });
            }}
          /> */}
        </Flex>
        <Flex itemAlign={"center"} gapX={2}>
          {isMobile && props.onReduceOnlyChange && (
            <ReduceOnlySwitch
              checked={props.reduceOnlyChecked ?? false}
              onCheckedChange={props.onReduceOnlyChange}
            />
          )}
          {!isMobile && (
            <TPSLAdvancedButton
              className={cn(
                "oui-group oui-invisible",
                props.switchState && "oui-visible",
              )}
              showTPSLAdvanced={props.showTPSLAdvanced}
            />
          )}
        </Flex>
      </Flex>
      <div
        className={cn(
          "oui-orderEntry-tpsl-body",
          "oui-overflow-hidden oui-transition-all",
          props.switchState ? "oui-max-h-[120px]" : "oui-max-h-0",
        )}
        onTransitionEnd={() => {
          console.log("transition end");
          tpslFormRef.current?.style.setProperty(
            "opacity",
            props.switchState ? "1" : "0",
          );
        }}
      >
        <TPSLInputForm
          ref={tpslFormRef}
          setOrderValue={props.setOrderValue}
          onChange={props.onChange}
          values={props.values}
          errors={props.errors}
          quote_dp={props.quote_dp}
          showTPSLAdvanced={props.showTPSLAdvanced}
          isMobile={isMobile}
          isSlPriceWarning={isSlPriceWarning}
        />
      </div>

      {isSlPriceWarning && <TPSLPriceWarning errors={props.errors} />}
    </div>
  );
};

const TPSLPriceWarning = (props: { errors: OrderValidationResult | null }) => {
  const { getErrorMsg } = useOrderEntryFormErrorMsg(props.errors);

  return (
    <DotStatus
      color="warning"
      size="xs"
      label={getErrorMsg("sl_trigger_price")}
    />
  );
};

const TPSLInputForm = React.forwardRef<
  HTMLDivElement,
  {
    setOrderValue: (key: string, value: any) => void;
    onChange: (key: OrderValueKeys, value: any) => void;
    values: TPSL_Values;
    errors: OrderValidationResult | null;
    quote_dp: number | undefined;
    showTPSLAdvanced: () => void;
    isMobile: boolean;
    isSlPriceWarning?: boolean;
  }
>((props, ref) => {
  const { getErrorMsg } = useOrderEntryFormErrorMsg(props.errors);
  const { t } = useTranslation();

  return (
    <div
      ref={ref}
      className={
        "oui-orderEntry-tpsl-form oui-space-y-1 oui-px-px oui-py-2 oui-transition-all"
      }
    >
      <Flex itemAlign={"center"} justify={"between"} gapX={2}>
        <TPSLPositionTypeWidget
          value={props.values.position_type}
          onChange={props.onChange}
        />
        {props.isMobile && (
          <TPSLAdvancedButton
            showTPSLAdvanced={props.showTPSLAdvanced}
            isMobile={props.isMobile}
          />
        )}
      </Flex>
      <PnlInputProvider values={props.values.tp} type={"TP"}>
        <TPSLInputRow
          type={"TP"}
          error={getErrorMsg("tp_trigger_price")}
          onChange={props.onChange}
          values={props.values.tp}
          quote_dp={props.quote_dp}
          testIds={{
            first: "oui-testid-orderEntry-tpsl-tpPrice-input",
            second: "oui-testid-orderEntry-tpsl-tpPnl-input",
            dropDown: "oui-testid-orderEntry-tpsl-tp-dropDown-trigger-button",
          }}
        />
      </PnlInputProvider>
      <PnlInputProvider values={props.values.sl} type={"SL"}>
        <TPSLInputRow
          type={"SL"}
          error={
            props.isSlPriceWarning ? undefined : getErrorMsg("sl_trigger_price")
          }
          displayErrorMessage={getErrorMsg("sl_trigger_price") !== undefined}
          onChange={props.onChange}
          values={props.values.sl}
          quote_dp={props.quote_dp}
          testIds={{
            first: "oui-testid-orderEntry-tpsl-slPrice-input",
            second: "oui-testid-orderEntry-tpsl-slPnl-input",
            dropDown: "oui-testid-orderEntry-tpsl-sl-dropDown-trigger-button",
          }}
          classNames={{
            root: props.isSlPriceWarning
              ? "oui-outline-warning-darken focus-within:oui-outline-warning-darken"
              : undefined,
          }}
        />
      </PnlInputProvider>
    </div>
  );
});

TPSLInputForm.displayName = "TPSLInputForm";

const TPSLAdvancedButton = (props: {
  showTPSLAdvanced: () => void;
  className?: string;
  isMobile?: boolean;
}) => {
  const { t } = useTranslation();

  return (
    <Flex
      itemAlign={"center"}
      gapX={1}
      onClick={props.showTPSLAdvanced}
      className={cn(
        "oui-orderEntry-tpsl-advanced-btn",
        "oui-group oui-cursor-pointer",
        props.className,
      )}
    >
      <Text
        className={cn(
          "oui-cursor-pointer group-hover:oui-text-base-contrast",
          props.isMobile ? "oui-text-2xs" : "oui-text-sm",
        )}
      >
        {t("tpsl.advanced")}
      </Text>
      <SettingFillIcon
        size={12}
        className="oui-advanced-icon oui-text-base-contrast-54 group-hover:oui-text-base-contrast oui-cursor-pointer"
        opacity={1}
        onClick={props.showTPSLAdvanced}
      />
    </Flex>
  );
};

//------- TPSLTriggerPriceInput start -------
const TPSLTriggerPriceInput = (props: {
  type: "TP" | "SL";
  error: string | undefined;
  displayErrorMessage?: boolean;
  values: Est_Values;
  onChange: (value: string) => void;
  quote_dp: number | undefined;
  testId?: string;
  classNames?: {
    root?: string;
    input?: string;
    prefix?: string;
  };
}) => {
  const { t } = useTranslation();
  const { errorMsgVisible } = useOrderEntryContext();
  const { tipsEle } = usePnlInputContext();
  const [placeholder, setPlaceholder] = useState<string>("USDC");

  const [tipVisible, setTipVisible] = useState(false);
  const [isFocused, setIsFocused] = useState(false);

  const [innerValue, setInnerValue] = useState<string>(
    props.values.trigger_price ?? "",
  );

  useEffect(() => {
    if (isFocused) {
      return;
    }
    setInnerValue(props.values.trigger_price ?? "");
  }, [props.values.trigger_price, isFocused]);

  const triggerPriceToolTipEle = useMemo(() => {
    if (props.error && (errorMsgVisible || props.displayErrorMessage))
      return props.error;
    if (tipVisible) return tipsEle;

    return null;
  }, [
    props.error,
    errorMsgVisible,
    tipVisible,
    tipsEle,
    props.displayErrorMessage,
  ]);

  const prefix =
    isFocused || innerValue
      ? props.type === "TP"
        ? t("tpsl.tp")
        : t("tpsl.sl")
      : props.type === "TP"
        ? t("tpsl.tpPrice")
        : t("tpsl.slPrice");

  const onValueChange = (value: string) => {
    setInnerValue(value);
    props.onChange(value);
  };

  const onFocus = () => {
    setPlaceholder("");
    setTipVisible(true);
    setIsFocused(true);
  };

  const onBlur = () => {
    setPlaceholder("USDC");
    setTipVisible(false);
    setIsFocused(false);
    props.onChange(innerValue);
  };

  return (
    <Input.tooltip
      data-testid={props.testId}
      prefix={prefix}
      size={"md"}
      placeholder={placeholder}
      align="right"
      onFocus={onFocus}
      onBlur={onBlur}
      tooltip={triggerPriceToolTipEle}
      tooltipProps={{
        content: {
          side: props.type === "TP" ? "top" : "bottom",
        },
      }}
      color={props.error ? "danger" : undefined}
      autoComplete={"off"}
      value={innerValue}
      classNames={{
        additional: "oui-text-base-contrast-54",
        root: cn(
          "oui-orderEntry-tpsl-triggerPrice",
          "oui-pe-2 md:oui-pe-3",
          props.classNames?.root,
        ),
        prefix: cn("oui-pe-1 md:oui-pe-2", props.classNames?.prefix),
        input: cn(
          "oui-text-2xs placeholder:oui-text-2xs",
          props.classNames?.input,
        ),
      }}
      // onChange={props.onChange}
      onValueChange={onValueChange}
      formatters={[
        inputFormatter.numberFormatter,
        inputFormatter.dpFormatter(props.quote_dp ?? 2),
        inputFormatter.currencyFormatter,
      ]}
    />
  );
};

//------- TPSLTriggerPriceInput end -------

const TPSLInputRow: React.FC<{
  type: "TP" | "SL";
  values: Est_Values;
  error?: string;
  displayErrorMessage?: boolean;
  onChange: (key: OrderValueKeys, value: any) => void;
  quote_dp: number | undefined;
  testIds?: {
    first?: string;
    second?: string;
    dropDown?: string;
  };
  classNames?: {
    root?: string;
    input?: string;
    prefix?: string;
  };
}> = (props) => {
  return (
    <Grid cols={2} gapX={1}>
      <TPSLTriggerPriceInput
        testId={props.testIds?.first}
        type={props.type}
        error={props.error}
        displayErrorMessage={props.displayErrorMessage}
        values={props.values ?? ""}
        classNames={props.classNames}
        onChange={(event) => {
          props.onChange(
            props.type === "SL" ? "sl_trigger_price" : "tp_trigger_price",
            event,
          );
        }}
        quote_dp={props.quote_dp}
      />

      <PnlInputWidget
        testIds={{
          input: props.testIds?.second,
          dropDown: props.testIds?.dropDown,
        }}
        onChange={props.onChange as any}
        quote={"USDC"}
        quote_dp={2}
        type={props.type}
        values={{
          PnL: props.values.PnL,
          Offset: props.values.Offset,
          "Offset%": props.values["Offset%"],
          OffsetFromMark: props.values.OffsetFromMark,
          PercentageFromMark: props.values.PercentageFromMark,
          ROI: props.values.ROI,
        }}
      />
    </Grid>
  );
};
