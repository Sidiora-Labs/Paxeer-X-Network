import { useBecomeAffiliateScript } from "./becomeAffiliate.script";
import { BecomeAffiliate } from "./becomeAffiliate.ui";

export const BecomeAffiliateWidget = () => {
  const state = useBecomeAffiliateScript();
  return <BecomeAffiliate {...state} />;
};
