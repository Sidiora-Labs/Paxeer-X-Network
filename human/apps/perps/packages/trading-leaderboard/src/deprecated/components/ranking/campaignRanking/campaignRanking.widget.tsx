import { FC } from "react";
import { Ranking, RankingProps } from "../shared/ranking.ui";
import {
  CampaignRankingScriptOptions,
  useCampaignRankingScript,
} from "./campaignRanking.script";

export type CampaignRankingWidgetProps = Pick<
  RankingProps,
  "style" | "className" | "fields"
> &
  CampaignRankingScriptOptions;

export const CampaignRankingWidget: FC<CampaignRankingWidgetProps> = (
  props,
) => {
  const { campaignId, fields, sortKey, ...rest } = props;
  const state = useCampaignRankingScript({
    campaignId,
    sortKey,
  });

  // @ts-ignore
  return <Ranking {...state} {...rest} fields={fields} type="campaign" />;
};
