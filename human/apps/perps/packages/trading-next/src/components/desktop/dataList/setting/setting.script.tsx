import { useReversePositionEnabled } from "@paxeer-network/ui-positions";
import { SettingWidgetProps } from "./setting.widget";

export const useSettingScript = (props: SettingWidgetProps) => {
  const { isEnabled, setEnabled } = useReversePositionEnabled();
  return {
    ...props,
    reversePosition: isEnabled,
    setReversePosition: setEnabled,
  };
};

export type SettingState = ReturnType<typeof useSettingScript>;
