import { useReferralInfoScript } from "./referralInfo.script";
import { ReferralInfo } from "./referralInfo.ui";

export const ReferralInfoWidget = () => {
  const state = useReferralInfoScript();
  return <ReferralInfo {...state} />;
};
