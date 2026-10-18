import { useTitleStatisticScript } from "./titleStatistic.script";
import { TitleStatistic } from "./titleStatistic.ui";

export const TitleStatisticWidget = () => {
  const state = useTitleStatisticScript();
  return <TitleStatistic {...state} />;
};
