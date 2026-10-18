import { useReferralLinkScript } from "./referralLink.script";
import { ReferralLink } from "./referralLink.ui";

export const ReferralLinkWidget = () => {
  const state = useReferralLinkScript();
  return <ReferralLink {...state} />;
};
