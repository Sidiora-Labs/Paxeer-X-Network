import { TipsUi } from "./tips.ui";
import { useTipsScript } from "./tips.script";

export function TipsWidget() {
  const state = useTipsScript();
  return (
    <TipsUi {...state}/>
  )
}