import React from "react";
import { MarginMode } from "@paxeer-network/types";
import { registerSimpleDialog, registerSimpleSheet } from "@paxeer-network/ui";
import { useTPSLSimpleDialog } from "./tpslSimpleDialog.script";
import { TPSLSimpleDialogUI } from "./tpslSimpleDialog.ui";

export const TPSLSimpleDialogWidget: React.FC<{
  type: "tp" | "sl";
  triggerPrice?: number;
  symbol: string;
  close?: () => void;
  onComplete?: () => void;
  marginMode?: MarginMode;
  showAdvancedTPSLDialog?: () => void;
}> = (props) => {
  const { close, onComplete, showAdvancedTPSLDialog } = props;
  const state = useTPSLSimpleDialog(props);

  return (
    <TPSLSimpleDialogUI
      {...state}
      close={close}
      onComplete={onComplete}
      showAdvancedTPSLDialog={showAdvancedTPSLDialog}
    />
  );
};

export const TPSLSimpleSheetId = "TPSLSimpleSheetId";

export const TPSLSimpleDialogId = "TPSLSimpleDialogId";

registerSimpleSheet(TPSLSimpleSheetId, TPSLSimpleDialogWidget, {
  classNames: {},
});

registerSimpleDialog(TPSLSimpleDialogId, TPSLSimpleDialogWidget, {
  classNames: {
    content: "oui-w-[420px]",
  },
});
