import { useScreen } from "@paxeer-network/ui";
import { useHistoryDataGroupScript } from "./historyDataGroup.script";
import { HistoryDataGroupDesktop } from "./historyDataGroup.ui.desktop";
import { HistoryDataGroupMobile } from "./historyDataGroup.ui.mobile";

export const HistoryDataGroupWidget: React.FC = () => {
  const state = useHistoryDataGroupScript();
  const { isMobile } = useScreen();
  if (isMobile) {
    return <HistoryDataGroupMobile {...state} />;
  }
  return <HistoryDataGroupDesktop {...state} />;
};
