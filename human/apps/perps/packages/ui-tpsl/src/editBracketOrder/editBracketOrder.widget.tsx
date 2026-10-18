import { API } from "@paxeer-network/types";
import { registerSimpleDialog, registerSimpleSheet } from "@paxeer-network/ui";
import { useEditBracketOrder } from "./editBracketOrder.script";
import { EditBracketOrderUI } from "./editBracketOrder.ui";

export const EditBracketOrderWidget = (props: {
  order: API.AlgoOrderExt;
  close?: () => void;
}) => {
  const state = useEditBracketOrder({ order: props.order });
  return <EditBracketOrderUI {...state} onClose={props.close} />;
};

export const EditBracketOrderSheetId = "EditBracketOrderSheetId";
export const EditBracketOrderDialogId = "EditBracketOrderDialogId";

registerSimpleSheet(EditBracketOrderSheetId, EditBracketOrderWidget);

registerSimpleDialog(EditBracketOrderDialogId, EditBracketOrderWidget, {
  classNames: {
    content: "oui-w-[420px]",
  },
});
