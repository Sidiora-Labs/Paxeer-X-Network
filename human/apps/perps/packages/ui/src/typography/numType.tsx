import { formatNum } from "@paxeer-network/utils";
import { Numeral, NumeralProps } from "./numeral";

type NumTypeProps = Omit<NumeralProps, "ignoreDP" | "rm" | "tick"> & {
  numType: "roi" | "pnl" | "notional" | "assetValue" | "collateral";
};

type NumTypeComponentProps = Omit<NumTypeProps, "numType">;

const NumType: React.FC<NumTypeProps> = (props) => {
  const { numType, dp = 2, children, ...rest } = props;

  const renderChild = () => {
    switch (numType) {
      case "roi":
        return formatNum
          .roi(children, props.rule === "percentages" ? 4 : 2)
          ?.toString();
      case "pnl":
        return formatNum.pnl(children);
      case "notional":
        return formatNum.notional(children);
      case "assetValue":
        return formatNum.assetValue(children);
      case "collateral":
        return formatNum.collateral(children);
    }
  };

  return (
    <Numeral dp={dp} rm={undefined} tick={undefined} {...(rest as any)}>
      {renderChild() ?? "--"}
    </Numeral>
  );
};

export const NumTypeRoi: React.FC<NumTypeComponentProps> = (props) => {
  return <NumType {...props} numType="roi" />;
};

export const NumTypePnl: React.FC<NumTypeComponentProps> = (props) => {
  return <NumType {...props} numType="pnl" />;
};

export const NumTypeNotional: React.FC<NumTypeComponentProps> = (props) => {
  return <NumType {...props} numType="notional" />;
};

export const NumTypeAssetValue: React.FC<NumTypeComponentProps> = (props) => {
  return <NumType {...props} numType="assetValue" />;
};

export const NumTypeCollateral: React.FC<NumTypeComponentProps> = (props) => {
  return <NumType {...props} numType="collateral" />;
};
