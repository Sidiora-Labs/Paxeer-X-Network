import { useEffect, useState } from "react";
import {
  MaintenanceStatus,
  useLocalStorage,
  useMaintenanceStatus,
  useMarketList,
  usePaxeerNetworkContext,
  useQuery,
  useWS,
} from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { useAppContext } from "@paxeer-network/react-app";
import { AnnouncementType, API, WSMessage } from "@paxeer-network/types";
import {
  filterDuplicateArrayById,
  getTimeString,
  shouldShowListingDelistingAnnouncement,
  sortDataByUpdatedTime,
} from "../utils";

const maintentanceId = "-1";

export type AnnouncementOptions = {
  hideTips?: boolean;
};

interface AnnouncementStore {
  show?: boolean;
  lastUpdateTime?: number | null;
}

const PAXEER_NETWORK_ANNOUNCEMENT_KEY = "paxeerNetwork_announcement";

const useAnnouncementData = () => {
  const { t } = useTranslation();
  const ws = useWS();
  const { dataAdapter } = usePaxeerNetworkContext();
  const marketList = useMarketList();

  const [announcementStore, setAnnouncementStore] =
    useLocalStorage<AnnouncementStore>(PAXEER_NETWORK_ANNOUNCEMENT_KEY, {});

  const [tips, setTips] = useState<API.AnnouncementRow[]>([]);
  const [maintenances, setMaintenances] = useState([] as API.AnnouncementRow[]);

  const [maintenanceDialogInfo, setMaintenanceDialogInfo] = useState<string>();

  const { startTime, endTime, status, brokerName } = useMaintenanceStatus();

  const { data: announcements } = useQuery<API.Announcement>(
    `/v2/public/announcement`,
    {
      revalidateOnFocus: false,
      refreshInterval: 60 * 60 * 1000, // refresh every 1 hour
      formatter: (data) => data,
    },
  );

  const getMaintentTipsContent = (
    brokerName: string,
    startDate: string,
    endDate: string,
  ) => t("maintenance.tips.description", { brokerName, startDate, endDate });

  const getMaintentDialogContent = (brokerName: string, endDate: string) =>
    t("maintenance.dialog.description", { brokerName, endDate });

  useEffect(() => {
    const startDate = startTime ? getTimeString(startTime) : "-";
    const endDate = endTime ? getTimeString(endTime) : "-";

    const dialogContent =
      status === MaintenanceStatus.Maintenance
        ? getMaintentDialogContent(brokerName, endDate)
        : undefined;

    setMaintenanceDialogInfo(dialogContent);

    if (startTime && endTime) {
      setMaintenances([
        {
          announcement_id: maintentanceId,
          type: AnnouncementType.Maintenance,
          startTime: startTime,
          endTime: endTime,
          updated_time: startTime,
          message: getMaintentTipsContent(brokerName, startDate, endDate),
        },
      ] as unknown as API.AnnouncementRow[]);
    } else {
      setMaintenances([]);
    }
  }, [startTime, endTime, status, brokerName, t]);

  useEffect(() => {
    let list = [...(maintenances ?? []), ...(announcements?.rows ?? [])];

    if (typeof dataAdapter?.announcementList === "function") {
      list = dataAdapter.announcementList(list);
    }

    list = list.filter((item) =>
      shouldShowListingDelistingAnnouncement(item, marketList),
    );

    const removedDuplicateList = filterDuplicateArrayById(list);
    const sortedList = sortDataByUpdatedTime(removedDuplicateList);

    setTips(sortedList);
  }, [maintenances, announcements, dataAdapter?.announcementList, marketList]);

  useEffect(() => {
    if (!announcements?.rows) {
      return;
    }
    const lastUpdateTime = announcements.last_updated_time ?? 0;
    const firstTipTime = tips[0]?.updated_time ?? 0;
    const updatedTime = Math.max(lastUpdateTime, firstTipTime);

    const closedTime = announcementStore.lastUpdateTime ?? 0;
    if (closedTime < updatedTime) {
      setAnnouncementStore({ show: true, lastUpdateTime: updatedTime });
    }
  }, [announcements, tips]);

  useEffect(() => {
    const unsubscribe = ws.subscribe("announcement", {
      onMessage(message: WSMessage.Announcement) {
        if (message) {
          const newTip: API.AnnouncementRow = {
            announcement_id: message.announcement_id,
            message: message.message,
            url: message.url,
            i18n: message.i18n,
            type: message.type,
            updated_time: message.updated_time,
          };
          if (!shouldShowListingDelistingAnnouncement(newTip, marketList)) {
            return;
          }
          setTips((prev) => {
            const list = prev.filter(
              (item) => item.announcement_id !== message.announcement_id,
            );
            return [...list, newTip];
          });
          // @ts-ignore
          setAnnouncementStore((prev) => ({ ...prev, show: true }));
        }
      },
    });
    return () => {
      unsubscribe?.();
    };
  }, [ws, marketList]);

  return {
    tips,
    maintenanceDialogInfo,
    announcementStore,
    setAnnouncementStore,
  };
};

export type AnnouncementReturn = ReturnType<typeof useAnnouncement>;

export const useAnnouncement = (options?: AnnouncementOptions) => {
  const { showAnnouncement, setShowAnnouncement } = useAppContext();

  const {
    tips,
    maintenanceDialogInfo,
    announcementStore,
    setAnnouncementStore,
  } = useAnnouncementData();

  const closeTips = () => {
    // @ts-ignore
    setAnnouncementStore((prev) => ({ ...prev, show: false }));
  };

  useEffect(() => {
    const len = tips.length;
    setShowAnnouncement(
      Boolean(len) && announcementStore.show && !options?.hideTips,
    );
  }, [tips, announcementStore.show, options?.hideTips, setShowAnnouncement]);

  return {
    maintenanceDialogInfo,
    tips,
    closeTips,
    showAnnouncement,
  };
};
