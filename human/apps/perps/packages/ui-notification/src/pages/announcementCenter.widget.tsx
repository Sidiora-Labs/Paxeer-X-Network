import { FC, useCallback } from "react";
import { useAnnouncementCenterScript } from "./announcementCenter.script";
import { AnnouncementCenterUI } from "./announcementCenter.ui";

export const AnnouncementCenterWidget: FC<{
  onRouteChange: (url: string) => void;
}> = (props) => {
  const { dataSource, current, setCurrent } = useAnnouncementCenterScript();
  const onItemClick = useCallback(
    (url: string) => {
      if (!url) return;
      props.onRouteChange(url);
    },
    [props.onRouteChange],
  );
  return (
    <AnnouncementCenterUI
      dataSource={dataSource}
      current={current}
      setCurrent={setCurrent}
      onItemClick={onItemClick}
    />
  );
};
