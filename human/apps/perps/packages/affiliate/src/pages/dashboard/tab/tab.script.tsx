import { ReactNode, useEffect, useMemo } from "react";
import { TabTypes, useReferralContext } from "../../../provider";

export type TabReturns = {
  tab: string;
  setTab: React.Dispatch<React.SetStateAction<TabTypes>>;
  isAffiliate: boolean;
  isTrader: boolean;
  isLoading: boolean;
  anAnAffiliate: () => void;
  anATrader: () => void;
  splashPage?: () => ReactNode | undefined;
  showHome: boolean;
  setShowHome: (value: boolean) => void;
};

export const useTabScript = (): TabReturns => {
  const {
    isAffiliate,
    isTrader,
    isLoading,
    splashPage,
    showHome,
    setShowHome,
    tab,
    setTab,
  } = useReferralContext();

  const tableValue = useMemo((): TabTypes => {
    if (isAffiliate && isTrader) {
      return tab;
    } else if (isAffiliate && !isTrader) {
      return TabTypes.affiliate;
    } else if (!isAffiliate && isTrader) {
      return TabTypes.trader;
    } else {
      return TabTypes.affiliate;
    }
  }, [tab, isAffiliate, isTrader]);

  useEffect(() => {
    const searchParams = new URLSearchParams(window.location.search);
    const tab = searchParams.get("tab");
    if (tab) {
      setTab(tab as TabTypes);
    }
  }, [window.location.search]);

  const anAnAffiliate = () => {
    // if (becomeAnAffiliateUrl !== undefined) {
    //   window.open(becomeAnAffiliateUrl, "_blank");
    // } else if (onBecomeAnAffiliate !== undefined) {
    //   onBecomeAnAffiliate?.();
    // }
    setShowHome(true);
  };

  const anATrader = () => {
    setShowHome(true);
    // showReferralPage?.();
  };

  return {
    setTab,
    tab: tableValue,
    isAffiliate,
    isTrader,
    isLoading,
    anAnAffiliate,
    anATrader,
    splashPage,
    showHome,
    setShowHome,
    // tab: TabTypes.affiliate,
    // isAffiliate: false,
    // isTrader: false,
    // isLoading: false,
  };
};
