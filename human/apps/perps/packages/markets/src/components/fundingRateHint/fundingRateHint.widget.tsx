import { useFundingRateHintScript } from "./fundingRateHint.script";
import { FundingRateHint, FundingRateHintProps } from "./fundingRateHint.ui";

export type FundingRateHintWidgetProps = Pick<
  FundingRateHintProps,
  "style" | "className"
>;

export const FundingRateHintWidget: React.FC<
  FundingRateHintWidgetProps & {
    symbol: string;
  }
> = (props) => {
  const state = useFundingRateHintScript(props.symbol);
  return <FundingRateHint {...state} {...props} />;
};
