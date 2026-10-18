import { useSummaryScript } from "./summary.script";
import { Summary } from "./summary.ui";

export const SummaryWidget = () => {
  const state = useSummaryScript();
  return <Summary {...state} />;
};
