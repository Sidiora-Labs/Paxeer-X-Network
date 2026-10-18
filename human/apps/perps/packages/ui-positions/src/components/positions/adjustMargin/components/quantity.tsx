import { FC, useCallback, useMemo } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { Flex, Input, Slider, Text, inputFormatter } from "@paxeer-network/ui";

export interface QuantityProps {
  inputValue: string;
  sliderValue: number;
  maxAmount: number;
  onInputChange: (value: string) => void;
  onSliderChange: (value: number) => void;
}

export const Quantity: FC<QuantityProps> = ({
  inputValue,
  sliderValue,
  maxAmount,
  onInputChange,
  onSliderChange,
}) => {
  const { t } = useTranslation();

  const percentMarks = useMemo(
    () => [0, 25, 50, 75, 100].map((m) => ({ value: m, label: `${m}%` })),
    [],
  );

  const isMaxClickable = maxAmount > 0;

  const handleSetToMax = useCallback(() => {
    if (!isMaxClickable) return;
    onSliderChange(100);
  }, [isMaxClickable, onSliderChange]);

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (e.key === "Enter" || e.key === " ") {
        e.preventDefault();
        handleSetToMax();
      }
    },
    [handleSetToMax],
  );

  return (
    <Flex direction="column" gap={3} className="oui-w-full">
      <Input
        value={inputValue}
        onValueChange={onInputChange}
        type="text"
        fullWidth
        size="lg"
        align="right"
        prefix={t("positions.adjustMargin.quantity")}
        suffix="USDC"
        formatters={[
          inputFormatter.numberFormatter,
          inputFormatter.dpFormatter(2),
        ]}
        disabled={maxAmount <= 0}
        autoComplete="off"
        classNames={{
          // No resting border: the fill surface bounds the input. Focus ring stays off (Input has focus-within:outline-primary-light by default)
          root: "oui-rounded-[6px] oui-bg-base-6 oui-outline oui-outline-1 oui-outline-offset-0 oui-outline-transparent focus-within:oui-outline-transparent",
          // keep value aligned to suffix side and match design emphasis
          input: "oui-text-sm oui-font-semibold oui-text-base-contrast-98",
          // override default additional padding so value can sit closer to suffix
          additional: "oui-px-0",
          prefix: "oui-ps-3 oui-pe-2 oui-text-sm oui-text-base-contrast-54",
          suffix: "oui-ps-2 oui-pe-3 oui-text-sm oui-text-base-contrast-54",
        }}
      />

      <Flex direction="column" gap={2} className="oui-w-full">
        <Slider
          value={[sliderValue]}
          onValueChange={(val) => onSliderChange(val[0])}
          max={100}
          min={0}
          step={1}
          color="primary"
          showTip
          tipFormatter={(v, _min, _max, percent) => `${percent.toFixed(0)}%`}
          marks={percentMarks}
          markLabelVisible={false}
          disabled={maxAmount <= 0}
        />
        <Flex justify="between" className="oui-w-full">
          <Text size="2xs" className="oui-text-primary">
            {`${sliderValue.toFixed(0)}%`}
          </Text>
          <Flex
            gap={1}
            itemAlign="baseline"
            role={isMaxClickable ? "button" : undefined}
            tabIndex={isMaxClickable ? 0 : undefined}
            onClick={isMaxClickable ? handleSetToMax : undefined}
            onKeyDown={isMaxClickable ? handleKeyDown : undefined}
            className={
              isMaxClickable
                ? "oui-cursor-pointer oui-opacity-90 hover:oui-opacity-100"
                : undefined
            }
          >
            <Text size="2xs" className="oui-text-primary">
              {t("positions.adjustMargin.max")}
            </Text>
            <Text.numeral size="2xs" intensity={54} dp={2}>
              {maxAmount}
            </Text.numeral>
          </Flex>
        </Flex>
      </Flex>
    </Flex>
  );
};
