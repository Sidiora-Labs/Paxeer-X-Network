import React from "react";
import { FeeTierWidget, FeeTierWidgetProps } from "./feeTier.widget";

export type FeeTierPageProps = FeeTierWidgetProps;

export const FeeTierPage: React.FC<FeeTierPageProps> = (props) => {
  return <FeeTierWidget {...props} />;
};
