import { useScaffoldContext } from "../scaffold/scaffoldContext";
import { MessageCenter } from "./msgCenter.ui";

export const MessageCenterWidget = () => {
  const { announcementState } = useScaffoldContext();

  return (
    <MessageCenter
      messages={announcementState.tips}
      maintenanceDialogInfo={announcementState.maintenanceDialogInfo}
      showAnnouncement={announcementState.showAnnouncement}
      closeTips={announcementState.closeTips}
    />
  );
};
