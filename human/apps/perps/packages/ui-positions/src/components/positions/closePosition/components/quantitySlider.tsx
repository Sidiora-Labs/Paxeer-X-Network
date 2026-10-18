import { useEffect, useState } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { Button, cn, Flex, Slider, Text } from "@paxeer-network/ui";

type QuantitySliderProps = {
  value: number;
  onValueChange: (value: number) => void;
  base_dp: number;
  max: number;
  onMax: () => void;
};

export const QuantitySlider = (props: QuantitySliderProps) => {
  const { t } = useTranslation();
  const [sliderValue, setSliderValue] = useState(props.value);

  useEffect(() => {
    setSliderValue(props.value);
  }, [props.value]);

  return (
    <>
      <Flex justify={"between"} width={"100%"} gap={2}>
        {[25, 50, 75, 100].map((e, index) => (
          <Button
            key={index}
            variant={"outlined"}
            size={"xs"}
            color="secondary"
            onClick={() => {
              props.onValueChange(e);
              setSliderValue(e);
            }}
            className={cn(
              "oui-w-1/4",
              sliderValue === e ? "oui-border-primary oui-text-primary" : "",
            )}
          >{`${e}%`}</Button>
        ))}
      </Flex>
      <Slider
        showTip
        markCount={4}
        min={0}
        max={100}
        value={[props.value]}
        color="primary"
        onValueChange={(value) => {
          props.onValueChange(value[0]);
          setSliderValue(value[0]);
        }}
      />
      <Flex width={"100%"} justify={"between"}>
        <Text color="primary" size="2xs">{`${props.value}%`}</Text>
        <Flex gap={1} onClick={props.onMax} className="oui-cursor-pointer">
          <Text size="2xs" color="primary">
            {t("common.max")}
          </Text>
          <Text.numeral
            intensity={54}
            size="2xs"
            dp={props.base_dp}
            padding={false}
          >
            {props.max}
          </Text.numeral>
        </Flex>
      </Flex>
    </>
  );
};
