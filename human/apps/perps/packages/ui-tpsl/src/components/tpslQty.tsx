import { memo, useEffect, useRef, useState } from "react";
import { utils } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { Flex, Input, Slider, Text } from "@paxeer-network/ui";
import { inputFormatter } from "@paxeer-network/ui";
import { cn } from "@paxeer-network/ui";
import { Decimal } from "@paxeer-network/utils";

export type TPSLQuantityProps = {
  maxQty: number;
  baseTick: number;
  base_dp: number;
  base: string;
  onQuantityChange?: (value: number | string) => void;
  /** Raw form state while editing may be string (e.g. "", "0."). */
  quantity: number | string;
  isEditing?: boolean;
  disabled?: boolean;
  errorMsg?: string;
};

export const TPSLQuantity = memo<TPSLQuantityProps>((props) => {
  const { maxQty, base_dp, baseTick, quantity } = props;
  const inputRef = useRef<HTMLInputElement>(null);

  const { t } = useTranslation();
  const [sliderValue, setSliderValue] = useState(0);

  // format quantity to base tick
  const formatQuantity = (value: string) => {
    if (props.disabled) return;
    let _qty = value;
    if (Number(value) > maxQty) {
      _qty = maxQty.toString();
    }
    if (baseTick > 0) {
      props.onQuantityChange?.(utils.formatNumber(_qty, baseTick) ?? value);
    }
  };

  const onSliderValueChange = (value: number) => {
    if (props.disabled) return;
    setSliderValue(value);
    // transform slider value to quantity
    const qty = new Decimal(value)
      .div(100)
      .mul(maxQty)
      .toFixed(base_dp, Decimal.ROUND_DOWN);

    formatQuantity(qty);
  };

  useEffect(() => {
    const qty = Math.min(Number(quantity || 0), maxQty);
    // transform quantity to slider value
    const slider = new Decimal(qty)
      .div(maxQty)
      .mul(100)
      .toDecimalPlaces(2, Decimal.ROUND_DOWN)
      .toNumber();

    setSliderValue(slider);
  }, [quantity, maxQty]);

  const errorMsg =
    props.quantity.toString().length > 0 ? props.errorMsg : undefined;

  return (
    <>
      <Flex gap={2}>
        <div className={"oui-flex-1"}>
          <Input.tooltip
            data-testid="oui-testid-tpsl-popUp-quantity-input"
            ref={inputRef}
            prefix={t("common.quantity")}
            size={{
              initial: "md",
              lg: "sm",
            }}
            align="right"
            value={props.quantity}
            disabled={props.disabled}
            autoComplete="off"
            classNames={{
              prefix: "oui-text-base-contrast-54",
              root: cn(
                "oui-bg-base-5 oui-outline-line-12",
                errorMsg && "oui-outline-danger",
              ),
            }}
            tooltipProps={{
              content: {
                className: "oui-bg-base-6 oui-text-base-contrast-80",
              },
              arrow: {
                className: "oui-fill-base-6",
              },
            }}
            tooltip={errorMsg}
            color={errorMsg ? "danger" : undefined}
            formatters={[
              inputFormatter.decimalPointFormatter,
              inputFormatter.dpFormatter(props.base_dp),
              inputFormatter.numberFormatter,
              inputFormatter.currencyFormatter,
            ]}
            onValueChange={(value) => {
              if (props.disabled) return;
              props.onQuantityChange?.(value);
              // TODO: optimize this
              const qty = Number(value);
              if (qty && qty > props.maxQty) {
                const qty = props.maxQty;
                props.onQuantityChange?.(qty);
                inputRef.current?.blur();
              }
            }}
            onBlur={(e) => formatQuantity(e.target.value)}
            suffix={
              <span className="oui-px-3 oui-text-2xs oui-text-base-contrast-54">
                {props.base}
              </span>
            }
          />
        </div>
      </Flex>
      <Flex mt={2} itemAlign={"center"} height={"15px"}>
        <Slider
          min={0}
          max={100}
          markCount={5}
          showTip
          value={[sliderValue]}
          color="primary"
          disabled={props.disabled}
          onValueChange={(value) => {
            onSliderValueChange(value[0]);
          }}
        />
      </Flex>
      <Flex justify={"between"}>
        <Text.numeral color={"primary"} size={"2xs"} suffix="%">
          {sliderValue}
        </Text.numeral>
        <Flex itemAlign={"center"} gap={1}>
          <button
            className={"oui-leading-none"}
            disabled={props.disabled}
            style={{ lineHeight: 0 }}
            onClick={() => {
              if (!props.disabled) {
                props.onQuantityChange?.(props.maxQty);
              }
            }}
          >
            <Text color={"primary"} size={"2xs"}>
              {t("common.max")}
            </Text>
          </button>

          <Text.numeral
            rule={"price"}
            size={"2xs"}
            intensity={54}
            tick={props.baseTick}
          >
            {props.maxQty}
          </Text.numeral>
        </Flex>
      </Flex>
    </>
  );
});
