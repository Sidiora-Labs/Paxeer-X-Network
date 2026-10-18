import { memo, useEffect, useMemo, useState } from "react";
import { utils } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { OrderSide } from "@paxeer-network/types";
import { Flex, Slider, textVariants, Text } from "@paxeer-network/ui";
import { Decimal } from "@paxeer-network/utils";
import { InputType } from "../../types";
import { useOrderEntryContext } from "../orderEntryContext";

type QuantitySliderProps = {
  canTrade: boolean;
  side: OrderSide;
  order_quantity?: string;
  maxQty: number;
};
const SLIDER_MIN = 0;
const SLIDER_MAX = 100;

export const QuantitySlider = memo((props: QuantitySliderProps) => {
  const { canTrade, side, order_quantity, maxQty } = props;

  const [sliderValue, setSliderValue] = useState<number>(0);

  const {
    setOrderValue,
    manualSetOrderValue,
    symbolInfo,
    lastQuantityInputType,
  } = useOrderEntryContext();

  const { base_dp, base_tick } = symbolInfo;

  const { t } = useTranslation();

  const color = useMemo(
    () => (canTrade ? (side === OrderSide.BUY ? "buy" : "sell") : undefined),
    [side, canTrade],
  );

  const maxLabel = useMemo(() => {
    return side === OrderSide.BUY
      ? t("orderEntry.maxBuy")
      : t("orderEntry.maxSell");
  }, [side, t]);
  /**
   * @description Handle slider value change from user interaction.
   * Marks quantity updates as manual so that upstream consumers can
   * distinguish between automatic recalculations and deliberate user input.
   * @param value New slider percentage value (0 - 100).
   */
  const onSliderValueChange = (value: number) => {
    lastQuantityInputType.current = InputType.QUANTITY_SLIDER;
    setSliderValue(value);
    // user-initiated slider changes should be treated as manual updates
    sliderToQuantity(value, true);
  };

  const sliderToQuantity = (value: number, isManual: boolean = false) => {
    const newQty = new Decimal(value)
      .div(SLIDER_MAX)
      .mul(maxQty)
      .toFixed(base_dp, Decimal.ROUND_DOWN);
    if (isManual) {
      manualSetOrderValue?.(
        "order_quantity",
        utils.formatNumber(newQty, base_tick),
      );
    } else {
      setOrderValue("order_quantity", utils.formatNumber(newQty, base_tick));
    }
  };

  const onMax = () => {
    onSliderValueChange(SLIDER_MAX);
    // when previous slider value is max, quantity will not update by useEffect, so must set quantity manually to maxQty
    if (sliderValue === SLIDER_MAX) {
      sliderToQuantity(SLIDER_MAX, true);
    }
  };

  // update quantity when maxQty changes and slider was the last manual input
  useEffect(() => {
    if (lastQuantityInputType.current === InputType.QUANTITY_SLIDER) {
      sliderToQuantity(sliderValue);
    }
  }, [maxQty]);

  useEffect(() => {
    const quantityToSlider = () => {
      if (order_quantity && Number(order_quantity) !== 0 && maxQty !== 0) {
        return new Decimal(Math.min(Number(order_quantity), maxQty))
          .div(maxQty)
          .mul(SLIDER_MAX)
          .todp(2, Decimal.ROUND_DOWN)
          .toNumber();
      }
      return 0;
    };

    // update slider value when last quantity input type is not quantity slider
    if (lastQuantityInputType.current !== InputType.QUANTITY_SLIDER) {
      setSliderValue(quantityToSlider());
    }
  }, [order_quantity, maxQty]);

  return (
    <div className="oui-orderEntry-quantitySlider">
      <Slider
        disabled={maxQty === 0 || !canTrade}
        value={[sliderValue]}
        color={color}
        markCount={4}
        showTip
        onValueChange={(e) => {
          onSliderValueChange(e[0]);
        }}
        min={SLIDER_MIN}
        max={SLIDER_MAX}
      />
      <Flex
        justify={"between"}
        className="oui-quantitySlider-footer oui-pt-1 xl:oui-pt-2"
      >
        <Text.numeral
          size={"2xs"}
          color={color}
          dp={2}
          padding={false}
          suffix="%"
        >
          {canTrade ? sliderValue : 0}
        </Text.numeral>
        <Flex>
          <button
            className={textVariants({
              size: "2xs",
              className: "oui-quantitySlider-maxQty-btn oui-me-1",
            })}
            onClick={onMax}
            data-testid="oui-testid-orderEntry-maxQty-value-button"
          >
            {maxLabel}
          </button>
          <Text.numeral
            size={"2xs"}
            color={color}
            dp={base_dp}
            padding={false}
            className="oui-quantitySlider-maxQty-value"
            data-testid="oui-testid-orderEntry-maxQty-value"
          >
            {canTrade ? maxQty : 0}
          </Text.numeral>
        </Flex>
      </Flex>
    </div>
  );
});

QuantitySlider.displayName = "QuantitySlider";
