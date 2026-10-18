import { API } from "@paxeer-network/types";
import {
  useScreen,
  modal,
  registerSimpleDialog,
  registerSimpleSheet,
} from "@paxeer-network/ui";
import { useAdjustMarginScript } from "./adjustMargin.script";
import { AdjustMargin } from "./adjustMargin.ui";

/**
 * Legacy global registration of the adjust-margin dialog/sheet, kept for
 * backward compatibility with external consumers that open it via
 * `modal.show(AdjustMarginDialogId | AdjustMarginSheetId)`.
 *
 * Internal code no longer uses this path: the desktop/mobile position tables
 * render `AdjustMarginButton` instead, which wraps the same script/UI in a
 * locally controlled `SimpleDialog`/`SimpleSheet`. Keep both paths in sync
 * when changing dialog options or the widget's props.
 */
export const AdjustMarginDialogId = "AdjustMarginDialog";
export const AdjustMarginSheetId = "AdjustMarginSheet";

export type AdjustMarginWidgetProps = {
  position: API.PositionTPSLExt;
  symbol: string;
};

export const AdjustMarginWidget = (props: AdjustMarginWidgetProps) => {
  const { isMobile } = useScreen();
  const state = useAdjustMarginScript({
    position: props.position,
    symbol: props.symbol,
    close: () =>
      modal.hide(isMobile ? AdjustMarginSheetId : AdjustMarginDialogId),
  });

  return <AdjustMargin {...state} />;
};

registerSimpleDialog(AdjustMarginDialogId, AdjustMarginWidget, {
  title: undefined,
  closable: false,
  size: "sm",
});

registerSimpleSheet(AdjustMarginSheetId, AdjustMarginWidget, {
  title: undefined,
  closable: false,
});
