import { useAsTraderScript } from "./asTrader.script";
import { AsTrader } from "./asTrader.ui";

export const AsTraderWidget = () => {
  const state = useAsTraderScript();
  return <AsTrader {...state} />;
};
