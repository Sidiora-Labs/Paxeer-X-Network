import { useEffect, useState } from "react";
import { utils } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import {
  PopoverRoot,
  PopoverContent,
  PopoverTrigger,
  Flex,
  Text,
  Slider,
  Button,
  inputFormatter,
} from "@paxeer-network/ui";
import { Input } from "@paxeer-network/ui";
import { Decimal } from "@paxeer-network/utils";
import { usePositionsRowContext } from "../positionsRowContext";

export const QuantityInput = (props: { value: number }) => {
  const [sliderValue, setSliderValue] = useState<number>(100);
  const { baseDp, updateQuantity, quantity, baseTick } =
    usePositionsRowContext();

  const resetQuantity = (percent: number) => {
    formatQuantityToBaseTick(`${props.value * percent}`);
  };

  const formatQuantityToBaseTick = (value: string) => {
    if (baseTick && baseTick > 0) {
      // format quantity to baseTick
      const formatQty = utils.formatNumber(value, baseTick) ?? value;
      updateQuantity(formatQty);
    }
  };

  useEffect(() => {
    const maxQty = Math.abs(props.value);
    const qty = Math.min(Number(quantity || 0), maxQty);

    // transform quantity to slider value
    const slider = new Decimal(qty)
      .div(maxQty)
      .mul(100)
      .todp(2, Decimal.ROUND_DOWN)
      .toNumber();

    setSliderValue(slider);
  }, [quantity, props.value]);

  return (
    <PopoverRoot>
      <PopoverTrigger>
        <Input
          size="sm"
          formatters={[
            inputFormatter.numberFormatter,
            ...(baseDp ? [inputFormatter.dpFormatter(baseDp)] : []),
          ]}
          value={quantity}
          onBlur={(event) => formatQuantityToBaseTick(event.target.value)}
          onValueChange={updateQuantity}
        />
      </PopoverTrigger>
      <PopoverContent
        className="oui-w-[360px] oui-rounded-xl"
        align="start"
        side="bottom"
        onOpenAutoFocus={(event) => {
          event.preventDefault();
        }}
      >
        <Flex p={1} gap={2} width={"100%"} itemAlign={"start"}>
          <Text size="xs" intensity={98} className="oui-min-w-[40px]">
            {`${sliderValue}%`}
          </Text>
          <Flex direction={"column"} width={"100%"} gap={2}>
            <Slider
              markCount={4}
              value={[sliderValue]}
              onValueChange={(values) => {
                resetQuantity(values[0] / 100);
              }}
            />
            <PercentButtons onClick={resetQuantity} />
          </Flex>
        </Flex>
      </PopoverContent>
    </PopoverRoot>
  );
};

const PercentButtons = (props: { onClick: (value: number) => void }) => {
  const { t } = useTranslation();
  const list = [
    {
      label: "0%",
      value: 0,
    },
    {
      label: "25%",
      value: 0.25,
    },
    {
      label: "50%",
      value: 0.5,
    },
    {
      label: "75%",
      value: 0.75,
    },
    {
      label: t("common.max"),
      value: 1,
    },
  ];

  return (
    <Flex gap={2} width={"100%"}>
      {list.map((item, index) => {
        return (
          <Button
            key={index}
            variant="outlined"
            color="secondary"
            size="xs"
            onClick={(e) => {
              e.stopPropagation();
              props.onClick(item.value);
            }}
            className="oui-w-1/5"
          >
            {item.label}
          </Button>
        );
      })}
    </Flex>
  );
};
