import React from "react";
import { CollateralRatioUI } from "./collateralRatio.ui";

export const CollateralRatioWidget: React.FC<{ value: number }> = (props) => {
  return <CollateralRatioUI {...props} />;
};
