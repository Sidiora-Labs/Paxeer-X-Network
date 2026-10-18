import { FC } from "react";
import { Flex } from "@paxeer-network/ui";
import {
  AdjustMarginScriptProps,
  AdjustMarginState,
  AdjustMarginTab,
} from "./adjustMargin.script";
import Footer from "./components/fotter";
import { Infos } from "./components/infos";
import { MarginActions } from "./components/marginActions";
import { Quantity } from "./components/quantity";
import { SymbolInfo } from "./components/symbolInfo";
import Title from "./components/title";

export type { AdjustMarginScriptProps, AdjustMarginTab };

export const AdjustMargin: FC<AdjustMarginState> = (props) => {
  const {
    symbol,
    inputValue,
    sliderValue,
    maxAmount,
    currentMargin,
    liquidationPrice,
    effectiveLeverage,
    onTabChange,
    onInputChange,
    onSliderChange,
    close,
    isAdd,
  } = props;

  return (
    <Flex
      direction="column"
      className="oui-w-full oui-rounded-[12px] oui-bg-base-8 oui-font-semibold"
    >
      <Title close={close} />

      <div className="oui-w-full oui-pt-5">
        <SymbolInfo symbol={symbol} />

        <Flex direction="column" gap={4} className="oui-mt-4 oui-w-full">
          <MarginActions isAdd={isAdd} onTabChange={onTabChange} />

          <Quantity
            inputValue={inputValue}
            sliderValue={sliderValue}
            maxAmount={maxAmount}
            onInputChange={onInputChange}
            onSliderChange={onSliderChange}
          />

          <Infos
            currentMargin={currentMargin}
            liquidationPrice={liquidationPrice}
            effectiveLeverage={effectiveLeverage}
          />
        </Flex>
      </div>
      <Footer {...props} />
    </Flex>
  );
};
