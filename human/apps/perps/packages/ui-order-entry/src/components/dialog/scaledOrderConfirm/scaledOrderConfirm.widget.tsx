import { FC } from "react";
import { i18n } from "@paxeer-network/i18n";
import { registerSimpleDialog } from "@paxeer-network/ui";
import { useScaledOrderConfirmScript } from "./scaledOrderConfirm.script";
import {
  ScaledOrderConfirm,
  ScaledOrderConfirmProps,
} from "./scaledOrderConfirm.ui";

export type ScaledOrderConfirmWidgetProps = ScaledOrderConfirmProps;

export const ScaledOrderConfirmWidget: FC<ScaledOrderConfirmWidgetProps> = (
  props,
) => {
  const state = useScaledOrderConfirmScript(props);

  return <ScaledOrderConfirm {...props} {...state} />;
};

export const scaledOrderConfirmDialogId = "scaledOrderConfirm";

registerSimpleDialog(scaledOrderConfirmDialogId, ScaledOrderConfirmWidget, {
  size: "md",
  title: () => i18n.t("orderEntry.confirmScaledOrder"),
  contentProps: {
    // className: "oui-p-0",
  },
});
