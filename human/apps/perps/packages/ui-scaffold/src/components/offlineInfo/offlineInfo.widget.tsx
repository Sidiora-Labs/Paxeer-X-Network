import { useOfflineInfoScript } from "./offlineInfo.script";
import { OfflineInfo, OfflineInfoProps } from "./offlineInfo.ui";

export type OfflineInfoWidgetProps = Pick<
  OfflineInfoProps,
  "style" | "className" | "onRefresh" | "onStatusChange"
> & {
  hideDelay?: number; // Delay in ms to hide UI after network recovery, default 3000
};

export const OfflineInfoWidget: React.FC<OfflineInfoWidgetProps> = (props) => {
  const state = useOfflineInfoScript({
    opts: {
      onStatusChange: props.onStatusChange,
      hideDelay: props.hideDelay,
    },
  });
  return <OfflineInfo {...state} {...props} />;
};
