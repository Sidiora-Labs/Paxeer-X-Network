import { useReferralCodesScript } from "./referralCodes.script";
import { ReferralCodes } from "./referralCodes.ui";

export const ReferralCodesWidget = () => {
  const state = useReferralCodesScript();
  return <ReferralCodes {...state} />;
};
