import { registerSimpleDialog } from "@paxeer-network/ui";
import { BindReferralCodeWidget } from "./bindReferralCode.widget";

export const BindReferralCodeDialogId = "BindReferralCodeDialogId";

registerSimpleDialog(BindReferralCodeDialogId, BindReferralCodeWidget, {
  size: "sm",
  classNames: {
    content: "oui-border oui-border-line-6",
    body: "!oui-pt-3",
  },
});
