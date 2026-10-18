import { useCommissionChartScript } from "./commissionChart.script";
import { CommissionChart } from "./commissionChart.ui";

export const CommissionChartWidget = () => {
  const state = useCommissionChartScript();
  return <CommissionChart {...state} />;
};
