import { registerSimpleDialog, registerSimpleSheet } from "@paxeer-network/ui";
import { ReferralCodeFormWidget } from "./referralCodeForm.widget";

export const ReferralCodeFormDialogId = "ReferralCodeFormDialogId";
export const ReferralCodeFormSheetId = "ReferralCodeFormSheetId";

registerSimpleDialog(ReferralCodeFormDialogId, ReferralCodeFormWidget, {
  size: "sm",
  classNames: {
    content: "oui-border oui-border-line-6",
    body: "!oui-pt-3",
  },
});

registerSimpleSheet(ReferralCodeFormSheetId, ReferralCodeFormWidget);
