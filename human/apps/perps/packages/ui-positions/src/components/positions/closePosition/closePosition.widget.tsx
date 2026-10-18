import { useScreen } from "@paxeer-network/ui";
import {
  ClosePositionScriptProps,
  useClosePositionScript,
} from "./closePosition.script";
import { DesktopClosePosition } from "./closePosition.ui";
import { MobileClosePosition } from "./closePositions.mobile.ui";

type ClosePositionWidgetProps = Pick<ClosePositionScriptProps, "type">;

export const ClosePositionWidget = (props: ClosePositionWidgetProps) => {
  const state = useClosePositionScript(props);
  const { isMobile } = useScreen();

  if (isMobile) {
    return <MobileClosePosition {...state} />;
  }

  return <DesktopClosePosition {...state} />;
};
