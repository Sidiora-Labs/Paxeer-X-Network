import { useSubtitleScript } from "./subtitle.script";
import { Subtitle } from "./subtitle.ui";

export const SubtitleWidget = () => {
  const state = useSubtitleScript();
  return <Subtitle {...state} />;
};
