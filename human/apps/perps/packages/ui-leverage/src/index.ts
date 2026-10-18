import { i18n } from "@paxeer-network/i18n";
import { registerSimpleDialog, registerSimpleSheet } from "@paxeer-network/ui";
import { LeverageEditor } from "./leverage.widget";

export { LeverageEditor, type LeverageEditorProps } from "./leverage.widget";
export { Leverage, LeverageSlider, LeverageHeader } from "./leverage.ui";
export type {
  LeverageProps,
  LeverageSliderProps,
  LeverageHeaderProps,
} from "./leverage.ui";
export {
  useLeverageScript,
  type LeverageScriptReturns,
} from "./leverage.script";
export * from "./symbolLeverage";

export const LeverageWidgetWithDialogId = "LeverageWidgetWithDialog";
export const LeverageWidgetWithSheetId = "LeverageWidgetWithSheet";

registerSimpleDialog(LeverageWidgetWithDialogId, LeverageEditor, {
  title: () => i18n.t("leverage.maxAccountLeverage"),
  size: "md",
});

registerSimpleSheet(LeverageWidgetWithSheetId, LeverageEditor, {
  title: () => i18n.t("leverage.maxAccountLeverage"),
});
