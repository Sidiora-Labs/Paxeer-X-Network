import { memo, useMemo } from "react";
import { useThrottledCallback, utils } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { Flex, Slider, Text } from "@paxeer-network/ui";
import { Decimal } from "@paxeer-network/utils";
import { useEditSheetContext } from "./editSheetContext";

type QuantitySliderProps = {
  maxQty: number;
  quantity: string | number;
};

export const QuantitySlider = memo((props: QuantitySliderProps) => {
  const { quantity, maxQty } = props;
  const { t } = useTranslation();

  const { symbolInfo, setOrderValue } = useEditSheetContext();
  const { base_dp, base_tick } = symbolInfo;

  const sliderValue = useMemo(() => {
    if (quantity && Number(quantity) !== 0 && maxQty !== 0) {
      const value = new Decimal(quantity)
        .div(maxQty)
        .mul(100)
        .toDecimalPlaces(2, Decimal.ROUND_DOWN)
        .toNumber();
      return value;
    }
    return 0;
  }, [quantity, maxQty]);

  const sliderToQuantity = useThrottledCallback(
    (value: number) => {
      const newQty = new Decimal(value)
        .div(100)
        .mul(maxQty)
        .toDecimalPlaces(base_dp, Decimal.ROUND_DOWN)
        .toNumber();

      setOrderValue("order_quantity", utils.formatNumber(newQty, base_tick));
    },
    50,
    {},
  );

  const percentages =
    props.quantity && props.maxQty
      ? Math.min(Number(props.quantity) / props.maxQty, 1)
      : undefined;

  return (
    <>
      <Slider
        markCount={4}
        value={[sliderValue]}
        onValueChange={(e) => {
          sliderToQuantity(e[0]);
        }}
        color="primary"
      />
      <Flex width={"100%"} justify={"between"}>
        <Text.numeral
          color="primary"
          size="2xs"
          dp={2}
          padding={false}
          rule="percentages"
        >{`${percentages ?? 0}`}</Text.numeral>
        <Flex
          gap={1}
          onClick={() => {
            setOrderValue("order_quantity", maxQty.toString());
          }}
        >
          <Text size="2xs" color="primary">
            {t("common.max")}
          </Text>
          <Text.numeral intensity={54} size="2xs" dp={base_dp}>
            {props.maxQty}
          </Text.numeral>
        </Flex>
      </Flex>
    </>
  );
});

QuantitySlider.displayName = "QuantitySlider";
