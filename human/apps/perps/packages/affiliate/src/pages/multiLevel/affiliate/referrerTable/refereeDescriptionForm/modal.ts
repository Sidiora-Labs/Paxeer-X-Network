import { registerSimpleDialog } from "@paxeer-network/ui";
import { RefereeDescriptionFormWidget } from "./refereeDescriptionForm.widget";

export const RefereeDescriptionFormDialogId = "RefereeDescriptionFormDialogId";

registerSimpleDialog(
  RefereeDescriptionFormDialogId,
  RefereeDescriptionFormWidget,
  {
    size: "sm",
    classNames: {
      content: "oui-border oui-border-line-6 !oui-max-w-[360px] !oui-px-0",
      body: "!oui-py-0",
    },
  },
);
