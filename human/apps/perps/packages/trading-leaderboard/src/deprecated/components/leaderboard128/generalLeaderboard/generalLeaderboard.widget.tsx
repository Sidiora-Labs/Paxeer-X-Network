import { FC } from "react";
import { useGeneralLeaderboardIScript } from "./generalLeaderboard.script";
import {
  GeneralLeaderboardI,
  GeneralLeaderboardIProps,
} from "./generalLeaderboard.ui";

export type GeneralLeaderboardIWidgetProps = Pick<
  GeneralLeaderboardIProps,
  "style" | "className" | "campaignDateRange" | "weekOneAddresses"
>;

export const GeneralLeaderboardIWidget: FC<GeneralLeaderboardIWidgetProps> = (
  props,
) => {
  const state = useGeneralLeaderboardIScript({
    campaignDateRange: props.campaignDateRange,
    weekOneAddresses: props.weekOneAddresses,
  });

  return (
    <GeneralLeaderboardI
      {...state}
      className={props.className}
      style={props.style}
    />
  );
};
