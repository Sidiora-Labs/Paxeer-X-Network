import { FC } from "react";
import {
  useReferralCodesTableScript,
  UseReferralCodesTableScriptProps,
} from "./referralCodesTable.script";
import { ReferralCodesTableUI } from "./referralCodesTable.ui";

export const ReferralCodesTableWidget: FC<UseReferralCodesTableScriptProps> = (
  props,
) => {
  const state = useReferralCodesTableScript(props);
  return <ReferralCodesTableUI {...state} />;
};
