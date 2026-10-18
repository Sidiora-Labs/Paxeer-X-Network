import { ReferralCodeFormField, ReferralCodeFormType } from "../../../../types";
import { useReferralCodeFormScript } from "./referralCodeForm.script";
import { ReferralCodeForm } from "./referralCodeForm.ui";

export type ReferralCodeFormWidgetProps = {
  type: ReferralCodeFormType;
  close?: () => void;
  onSuccess?: () => void | Promise<void>;
  referralCode?: string;
  maxRebateRate: number;
  bonusMaxRebateRate?: number;
  baseRebateRate?: number;
  defaultBonusRefereeRebateRate?: number;
  referrerRebateRate?: number;
  focusField?: ReferralCodeFormField;
  accountId?: string;
  directInvites?: number;
  directBonusRebateRate?: number;
};

export const ReferralCodeFormWidget = (props: ReferralCodeFormWidgetProps) => {
  const state = useReferralCodeFormScript(props);
  return <ReferralCodeForm {...state} {...props} />;
};
