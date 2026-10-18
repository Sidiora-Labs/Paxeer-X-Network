import { i18n } from "@paxeer-network/i18n";
import { registerSimpleDialog, registerSimpleSheet } from "@paxeer-network/ui";
import { SymbolLeverageWidget } from "./symbolLeverage.widget";

export {
  SymbolLeverageWidget,
  type SymbolLeverageWidgetProps,
} from "./symbolLeverage.widget";

export const SymbolLeverageSheetId = "SymbolLeverageSheetId";
export const SymbolLeverageDialogId = "SymbolLeverageDialogId";

// Register sheet version for mobile
registerSimpleSheet(SymbolLeverageSheetId, SymbolLeverageWidget, {
  title: () => i18n.t("leverage.adjustedLeverage"),
  classNames: {
    // content: "oui-p-5",
  },
});

// Register dialog version for desktop
registerSimpleDialog(SymbolLeverageDialogId, SymbolLeverageWidget, {
  title: () => i18n.t("leverage.adjustedLeverage"),
  classNames: {
    content: "oui-w-[420px]",
  },
});
