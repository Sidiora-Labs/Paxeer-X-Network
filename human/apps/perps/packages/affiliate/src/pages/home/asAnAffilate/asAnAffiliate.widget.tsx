import React from "react";
import { useAsAnAffiliateScript } from "./asAnAffiliate.script";
import { AsAnAffiliate } from "./asAnAffiliate.ui";

export const AsAnAffiliateWidget: React.FC = () => {
  const state = useAsAnAffiliateScript();
  return <AsAnAffiliate {...state} />;
};
