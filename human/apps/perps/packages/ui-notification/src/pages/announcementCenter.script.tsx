import { useState } from "react";
import { useAnnouncement } from "../hooks/useAnnouncement";

export const useAnnouncementCenterScript = () => {
  const { tips } = useAnnouncement();
  const [current, setCurrent] = useState<string | number | null>(null);

  return {
    dataSource: tips,
    current,
    setCurrent,
  };
};
