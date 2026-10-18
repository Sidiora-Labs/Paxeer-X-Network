import { useBindReferralCodeScript } from "./bindReferralCode.script";
import { BindReferralCode } from "./bindReferralCode.ui";

/** `skipped: true` when user confirms “not referred” without binding a code. */
export type BindReferralCodeSuccessPayload = {
  skipped: boolean;
};

export type BindReferralCodeWidgetProps = {
  close?: () => void;
  onSuccess?: (payload: BindReferralCodeSuccessPayload) => void | Promise<void>;
};

export const BindReferralCodeWidget = (props: BindReferralCodeWidgetProps) => {
  const state = useBindReferralCodeScript(props);
  return <BindReferralCode {...state} {...props} />;
};
