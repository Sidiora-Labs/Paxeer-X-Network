import { FC, useState } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { type API } from "@paxeer-network/types";
import {
  InjectableEmptyDataState,
  ScrollArea,
  Text,
} from "@paxeer-network/ui";
import { cn } from "@paxeer-network/ui";
import { AnnouncementItem } from "./announcementItem";

export const AnnouncementContent: FC<{
  dataSource: API.AnnouncementRow[];
  current: string | number | null;
  onExpandToggle: (id: string | number | null) => void;
  onItemClick: (url: string) => void;
  showDivider?: boolean;
}> = (props) => {
  const { dataSource, current, onExpandToggle, onItemClick } = props;
  const { t } = useTranslation();

  if (!Array.isArray(dataSource) || dataSource.length === 0) {
    return (
      <div className="oui-flex oui-h-[160px] oui-items-center oui-justify-center">
        <InjectableEmptyDataState title={t("notification.empty")} />
      </div>
    );
  }

  return (
    <div
      className={cn(
        "oui-flex oui-flex-col oui-space-y-1",
        props.showDivider &&
          "[&>*:not(:first-child)]:oui-border-t [&>*:not(:first-child)]:oui-border-line-12  [&>*:not(:first-child)]:oui-pt-1",
      )}
    >
      {dataSource.map((item) => (
        <AnnouncementItem
          url={item.url}
          onItemClick={onItemClick}
          key={item.announcement_id}
          message={item.message}
          updatedTime={item.updated_time ?? 0}
          expanded={current === item.announcement_id}
          type={item.type}
          showDivider={props.showDivider}
          onExpandToggle={() => {
            if (current === item.announcement_id) {
              onExpandToggle(null);
            } else {
              onExpandToggle(item.announcement_id);
            }
          }}
        />
      ))}
    </div>
  );
};

export const AnnouncementCenterUI: FC<{
  dataSource: API.AnnouncementRow[];
  onItemClick: (url: string) => void;
}> = (props) => {
  const { t } = useTranslation();
  const [expanded, setExpanded] = useState<string | number | null>(null);

  return (
    <>
      <div className="oui-px-5 oui-pt-4">
        <Text intensity={80} weight="bold">
          {t("notification.title")}
        </Text>
      </div>

      <ScrollArea className="oui-flex oui-h-[300px] oui-flex-col oui-space-y-1 oui-p-3">
        <AnnouncementContent
          dataSource={props.dataSource}
          current={expanded}
          onExpandToggle={setExpanded}
          onItemClick={props.onItemClick}
        />
      </ScrollArea>
    </>
  );
};
