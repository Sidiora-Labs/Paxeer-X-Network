import { useEffect, useState } from "react";
import { useAccount } from "@paxeer-network/hooks";
import { useAppContext } from "@paxeer-network/react-app";
import { AccountStatusEnum } from "@paxeer-network/types";
import { cn, Flex, Spinner } from "@paxeer-network/ui";
import { useReferralContext } from "../../provider";
import { LandingPage, MultiLevelAffiliatePage } from "../multiLevel";
import { TabWidget } from "./tab";

export const DashboardPage = (props: {
  classNames?: {
    root?: string;
    loadding?: string;
    home?: string;
    dashboard?: string;
  };
}) => {
  const { root, ...rest } = props.classNames || {};

  const { state } = useAccount();

  const { wrongNetwork, disabledConnect, initialized } = useAppContext();

  const { isMultiLevelEnabled, multiLevelRebateInfo, isAffiliate, isLoading } =
    useReferralContext();

  const loadingView = (
    <Flex justify={"center"} itemAlign={"center"} height={"100vh"}>
      <Spinner />
    </Flex>
  );

  const [delay, setDelay] = useState(!initialized);

  useEffect(() => {
    const timer = setTimeout(() => setDelay(false), 500);
    return () => clearTimeout(timer);
  }, []);

  // we don't know wallet not connect => connect status, so manually delay to hide loading
  if (state.validating || delay) {
    return loadingView;
  }

  if (
    wrongNetwork ||
    disabledConnect ||
    (state.status < AccountStatusEnum.EnableTrading &&
      state.status !== AccountStatusEnum.EnableTradingWithoutConnected)
  ) {
    return <LandingPage />;
  }

  if (isLoading) {
    return loadingView;
  }

  if (isMultiLevelEnabled) {
    /** If the user has a legacy referral code ( is an affiliate), show the multi-level affiliate page */
    if (multiLevelRebateInfo?.referral_code || isAffiliate) {
      return <MultiLevelAffiliatePage />;
    }

    /** If the user does not have a legacy or multi-level referral code, show the landing page */
    return <LandingPage />;
  }

  return (
    <div
      id="oui-affiliate-dashboard-page"
      className={cn("oui-w-full oui-tracking-tight", root)}
    >
      <TabWidget classNames={rest} />
    </div>
  );
};
