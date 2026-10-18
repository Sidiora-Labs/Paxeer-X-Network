import { FaucetUi } from "./faucet.ui";
import { useFaucetScript } from "./faucet.script";

export function FaucetWidget() {
  const state = useFaucetScript();
  return (
    <FaucetUi {...state}/>
  )
}