import { useRebatesScript } from "./rebates.script";
import { Rebates } from "./rebates.ui";

export const RebatesWidget = () => {
  const state = useRebatesScript();
  return <Rebates {...state} />;
};
