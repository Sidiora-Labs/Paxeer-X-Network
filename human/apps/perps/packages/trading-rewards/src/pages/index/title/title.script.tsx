import { ReactNode } from "react";
import { useTradingRewardsContext } from "../provider";

export type TitleConfig = {
  /// default is `Trading rewards`
  title?: string | ReactNode;
  /// default is `Trade with paxeer-network’s to earn ORDER.`
  subtitle?: string | ReactNode;
  /// default is `Learn more about paxeer-network Trading rewards Program in Trading rewards Docs`
  content?: string | ReactNode;
  /// default is { url: 'https://paxeer-network.network/docs/introduction/tokenomics/trading-rewards', target: "_blank"}
  docOpenOptions?: {
    url?: string;
    target?: string;
    features?: string;
  };
  brokerName?: string;
};

export const useTitleScript = (): TitleConfig => {

    const { titleConfig } = useTradingRewardsContext();

    return titleConfig;
};
