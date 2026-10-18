import { ReactNode, createContext, useContext } from "react";
import { RefferalAPI as API } from "@paxeer-network/hooks";
import { MultiLevelReferralData } from "../hooks/useMultiLevelReferralData";

export enum TabTypes {
  affiliate = "affiliate",
  trader = "trader",
}

export type UserVolumeType = {
  "1d_volume"?: number;
  "7d_volume"?: number;
  "30d_volume"?: number;
  all_volume?: number;
};

// export type OverwiteCard = {
//     ref?: BuildNode,
//     refClassName?: string,
//     refIcon?: ReactNode,

//     trader?: BuildNode,
//     traderClassName?: string,
//     traderIcon?: ReactNode,
// }

export type BuildNode = (state: ReferralContextReturns) => ReactNode;

export type Overwrite = {
  shortBrokerName?: string;
  brokerName?: string;
  ref?: {
    top?: BuildNode;
    card?: BuildNode;
    step?: BuildNode;
  };
};

export type ChartConfig = {
  // affiliate: {
  //     bar?: BarStyle,
  //     yAxis?: YAxis,
  //     xAxis?: XAxis,
  // },
  // trader: {
  //     bar?: BarStyle,
  //     yAxis?: YAxis,
  //     xAxis?: XAxis,
  // }
  affiliate: {
    bar?: any;
    yAxis?: any;
    xAxis?: any;
  };
  trader: {
    bar?: any;
    yAxis?: any;
    xAxis?: any;
  };
};

export type ReferralContextProps = {
  //** click become an affiliate, If this method is implemented, the `becomeAnAffiliateUrl` will not work */
  onBecomeAnAffiliate?: () => void;
  //** set become an affiliate url, default is `https://paxeer-network.network/` */
  becomeAnAffiliateUrl?: string;
  //** bind refferal code callback */
  bindReferralCodeState?: (
    success: boolean,
    error: any,
    hide: any,
    queryParams: any,
  ) => void;
  //** click learn affilate, if this method is implemented, the `learnAffilateUrl` will not work */
  onLearnAffiliate?: () => void;
  //** set learn affiliate url, default is `https://paxeer-network.network/` */
  learnAffiliateUrl?: string;
  //** default is `https://paxeer-network.network/` */
  referralLinkUrl: string;
  //** referral index page */
  showReferralPage?: () => void;
  // onEnterTraderPage?: (params?: any) => void,
  // onEnterAffiliatePage?: (params?: any) => void,
  //** tab + tab content */
  showDashboard?: () => void;
  //** col chart config */
  chartConfig?: ChartConfig;
  //** overwrite refferal */
  overwrite?: Overwrite;
  //** build a splash page, if not impletement, will be dispaly referral page */
  splashPage?: () => ReactNode;
};

export type ReferralContextReturns = {
  referralInfo?: API.ReferralInfo;
  isAffiliate: boolean;
  isTrader: boolean;
  mutate: any;
  userVolume?: UserVolumeType;
  dailyVolume?: API.DayliVolume[];
  isLoading: boolean;
  showHome: boolean;
  setShowHome: (value: boolean) => void;
  tab: TabTypes;
  setTab: React.Dispatch<React.SetStateAction<TabTypes>>;
  wrongNetwork: boolean;
  disabledConnect: boolean;
  generateCode: API.AutoGenerateCode | undefined;
} & ReferralContextProps &
  MultiLevelReferralData;

export const ReferralContext = createContext<ReferralContextReturns>(
  {} as ReferralContextReturns,
);

export const useReferralContext = () => {
  return useContext(ReferralContext);
};
