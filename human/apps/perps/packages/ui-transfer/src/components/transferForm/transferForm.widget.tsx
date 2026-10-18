import { FC } from "react";
import { i18n } from "@paxeer-network/i18n";
import { registerSimpleDialog } from "@paxeer-network/ui";
import { registerSimpleSheet } from "@paxeer-network/ui";
import {
  TransferFormScriptOptions,
  useTransferFormScript,
} from "./transferForm.script";
import { TransferForm } from "./transferForm.ui";

export const TransferDialogId = "TransferDialogId";
export const TransferSheetId = "TransferSheetId";

export type TransferFormWidgetProps = TransferFormScriptOptions;

export const TransferFormWidget = (props: TransferFormWidgetProps) => {
  const state = useTransferFormScript({
    toAccountId: props.toAccountId,
    token: props.token,
    close: props.close,
  });
  return <TransferForm {...state} />;
};

export const TransferWidget: FC<TransferFormWidgetProps> = (props) => {
  return (
    <TransferFormWidget
      close={props.close}
      toAccountId={props.toAccountId}
      token={props.token}
    />
  );
};

registerSimpleDialog(TransferDialogId, TransferWidget, {
  size: "md",
  classNames: {
    content: "oui-border oui-border-line-6",
  },
  title: () => i18n.t("common.transfer"),
});

registerSimpleSheet(TransferSheetId, TransferWidget, {
  title: () => i18n.t("common.transfer"),
});
