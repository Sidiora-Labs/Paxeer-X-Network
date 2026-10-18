import { useTipsScript } from "./tips.script";
import { TipsUi } from "./tips.ui";

export function TipsWidget() {
  const state = useTipsScript();
  return <TipsUi {...state} />;
}
