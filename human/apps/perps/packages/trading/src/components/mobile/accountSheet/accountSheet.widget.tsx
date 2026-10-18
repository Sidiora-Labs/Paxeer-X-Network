import { ReferralProps, TradingRewardsProps } from "../../../types/types";
import { useAccountSheetScript } from "./accountSheet.script";
import { AccountSheet } from "./accountSheet.ui";

export const AccountSheetWidget = (
  props: ReferralProps & TradingRewardsProps
) => {
  const state = useAccountSheetScript(props);
  return <AccountSheet {...state} />;
};
