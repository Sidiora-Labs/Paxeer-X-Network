import { useBalanceScript } from "./balance.script";
import { Balance } from "./balance.ui";

export const BalanceWidget = () => {
  const state = useBalanceScript();
  return <Balance {...state} />;
};
