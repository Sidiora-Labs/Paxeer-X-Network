import { useScaffoldContext } from "../scaffold/scaffoldContext";
import { NotificationUI } from "./notification.ui";

export const NotificationWidget = () => {
  const { announcementState } = useScaffoldContext();

  return (
    <NotificationUI
      dataSource={announcementState.tips}
      onClose={announcementState.closeTips}
      showAnnouncement={announcementState.showAnnouncement}
    />
  );
};
