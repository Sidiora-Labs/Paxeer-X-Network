import { useFaucetScript } from "./faucet.script";
import { FaucetUi } from "./faucet.ui";

export function FaucetWidget() {
  const state = useFaucetScript();
  return <FaucetUi {...state} />;
}
