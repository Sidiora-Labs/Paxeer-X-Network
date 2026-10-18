import { SharePnLOptions, SharePnLParams } from "../types/types";
import { useSharePnLScript } from "./sharePnL.script";
import { DesktopSharePnL, MobileSharePnL } from "./sharePnL.ui";

export const SharePnLBottomSheetWidget = (props: {
  hide?: () => void;
  pnl?: SharePnLOptions & SharePnLParams;
}) => {
  const state = useSharePnLScript({
    hide: props.hide,
    pnl: props.pnl,
  });
  return <MobileSharePnL {...state} />;
};

export const SharePnLDialogWidget = (props: {
  hide?: () => void;
  pnl?: SharePnLOptions & SharePnLParams;
}) => {
  const state = useSharePnLScript({
    hide: props.hide,
    pnl: props.pnl,
  });
  return <DesktopSharePnL {...state} />;
};
