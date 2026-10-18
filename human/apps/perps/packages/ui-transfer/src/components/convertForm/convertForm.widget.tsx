import React from "react";
// import pick from "ramda/es/pick";
import { i18n } from "@paxeer-network/i18n";
import {
  registerSimpleDialog,
  registerSimpleSheet,
  useModal,
} from "@paxeer-network/ui";
import type { ConvertFormScriptOptions } from "./convertForm.script";
import { useConvertFormScript } from "./convertForm.script";
import { ConvertFormUI } from "./convertForm.ui";

export const ConvertDialogId = "ConvertDialogId";
export const ConvertSheetId = "ConvertSheetId";

export const ConvertFormWidget: React.FC<ConvertFormScriptOptions> = (
  props,
) => {
  const state = useConvertFormScript(props);
  return <ConvertFormUI {...state} />;
};

const ConvertFormModalWidget: React.FC<ConvertFormScriptOptions> = (props) => {
  const { visible } = useModal();

  return visible ? <ConvertFormWidget {...props} /> : null;
};

registerSimpleDialog(ConvertDialogId, ConvertFormModalWidget, {
  size: "md",
  classNames: { content: "oui-border oui-border-line-6" },
  title: () => i18n.t("transfer.convert"),
});

registerSimpleSheet(ConvertSheetId, ConvertFormModalWidget, {
  title: () => i18n.t("transfer.convert"),
});
