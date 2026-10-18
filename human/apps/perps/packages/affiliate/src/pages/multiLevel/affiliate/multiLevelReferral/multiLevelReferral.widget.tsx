import { useMultiLevelReferralScript } from "./multiLevelReferral.script";
import { MultiLevelReferral } from "./multiLevelReferral.ui";

export const MultiLevelReferralWidget = () => {
  const state = useMultiLevelReferralScript();
  return <MultiLevelReferral {...state} />;
};
