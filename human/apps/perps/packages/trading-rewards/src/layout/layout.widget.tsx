import { FC, PropsWithChildren } from "react";
import { ScaffoldProps } from "@paxeer-network/ui-scaffold";
import { useTradingRewardsLayoutScript } from "./layout.script";
import { TradingRewardsLayout } from "./layout.ui";

export const TradingRewardsLayoutWidget: FC<
  PropsWithChildren<ScaffoldProps>
> = (props) => {
  const state = useTradingRewardsLayoutScript({
    current: props.leftSideProps?.current,
  });
  return (
    <TradingRewardsLayout {...state} {...props}>
      {props.children}
    </TradingRewardsLayout>
  );
};
