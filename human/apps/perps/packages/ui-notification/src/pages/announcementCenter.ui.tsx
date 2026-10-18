import { FC } from "react";
import { API } from "@paxeer-network/types";
import { AnnouncementContent } from "../components/announcementCenter/announcementCenter.ui";

export const AnnouncementCenterUI: FC<{
  dataSource: API.AnnouncementRow[];
  current: string | number | null;
  setCurrent: (current: string | number | null) => void;
  onItemClick: (url: string) => void;
}> = (props) => {
  const { dataSource, current, setCurrent, onItemClick } = props;
  return (
    <div className="oui-m-1 oui-rounded-xl oui-bg-base-9 oui-p-2">
      <AnnouncementContent
        dataSource={dataSource}
        current={current}
        onExpandToggle={setCurrent}
        onItemClick={onItemClick}
        showDivider
      />
    </div>
  );
};
