import { useCommissionAndRefereesScript } from "./commissionAndReferees.script";
import { CommissionAndReferees } from "./commissionAndReferees.ui";

export const CommissionAndRefereesWidget = () => {
  const state = useCommissionAndRefereesScript();
  return <CommissionAndReferees {...state} />;
};
