import { i18n } from "@paxeer-network/i18n";
import { PaxeerNetworkOrder } from "@paxeer-network/types";
import { registerSimpleDialog, registerSimpleSheet } from "@paxeer-network/ui";
import { TPSLAdvancedUI } from "./tpslAdvanced.ui";
import { useTPSLAdvanced } from "./useTPSLAdvanced.script";

type Props = {
  order: PaxeerNetworkOrder;
  setOrderValue: (key: string, value: any) => void;
  onSubmit: (formattedOrder: PaxeerNetworkOrder) => void;
  onClose: () => void;
  symbolLeverage?: number;
};

export const TPSLAdvancedWidget = (props: Props) => {
  const state = useTPSLAdvanced({
    order: props.order,
    setOrderValue: props.setOrderValue,
    onSubmit: props.onSubmit,
    onClose: props.onClose,
    symbolLeverage: props.symbolLeverage,
  });
  return <TPSLAdvancedUI {...state} />;
};

export const TPSLAdvancedSheetId = "TPSLAdvancedSheetId";
export const TPSLAdvancedDialogId = "TPSLAdvancedDialogId";

registerSimpleSheet(TPSLAdvancedSheetId, TPSLAdvancedWidget, {
  title: () => i18n.t("common.settings"),
});

registerSimpleDialog(TPSLAdvancedDialogId, TPSLAdvancedWidget, {
  title: () => i18n.t("common.settings"),
});
