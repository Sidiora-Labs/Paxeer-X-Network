import React from "react";
import { useAssetsChartScript } from "./assetsChart.script";
import { AssetsChart } from "./assetsChart.ui";

export const AssetsChartWidget: React.FC = () => {
  const state = useAssetsChartScript();
  return <AssetsChart {...state} />;
};
