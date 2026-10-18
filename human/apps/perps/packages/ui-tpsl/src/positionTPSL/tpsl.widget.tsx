import React from "react";
import { type AlgoOrderRootType } from "@paxeer-network/types";
import { registerSimpleDialog, registerSimpleSheet } from "@paxeer-network/ui";
import { TPSL, TPSLProps } from "./tpsl.ui";
import { TPSLBuilderOptions, useTPSLBuilder } from "./useTPSL.script";

export type TPSLWidgetProps = {
  onTPSLTypeChange?: (type: AlgoOrderRootType) => void;
} & TPSLBuilderOptions &
  TPSLProps;

export const TPSLWidget: React.FC<TPSLWidgetProps> = (props) => {
  const { onCancel, onComplete, ...rest } = props;
  const state = useTPSLBuilder(rest);

  return (
    <TPSL
      {...state}
      onCancel={onCancel}
      onComplete={onComplete}
      close={rest.close}
    />
  );
};

export const TPSLSheetId = "TPSLSheetId";
export const TPSLDialogId = "TPSLDialogId";

registerSimpleSheet(TPSLSheetId, TPSLWidget);

registerSimpleDialog(TPSLDialogId, TPSLWidget, {
  classNames: {
    content: "oui-w-[420px]",
  },
});
