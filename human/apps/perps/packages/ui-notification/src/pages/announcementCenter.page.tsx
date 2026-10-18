import { FC } from "react";
import { RouterAdapter } from "@paxeer-network/types";
import { AnnouncementCenterWidget } from "./announcementCenter.widget";

export const AnnouncementCenterPage: FC<{ routerAdapter?: RouterAdapter }> = (
  props,
) => {
  return (
    <AnnouncementCenterWidget
      onRouteChange={(url: string) => {
        if (!url) return;
        props.routerAdapter?.onRouteChange({
          href: url,
          name: url,
          target: "_blank",
        });
      }}
    />
  );
};
