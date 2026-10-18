import { useEffect, useState } from "react";
import {
  useAccount,
  useAccountInfo,
  useDebouncedCallback,
  useMutation,
  useLocalStorage,
  usePaxeerNetworkContext,
} from "@paxeer-network/hooks";
import { useAppContext } from "@paxeer-network/react-app";
import { AccountStatusEnum } from "@paxeer-network/types";
import { toast, usePaxeerNetworkTheme } from "@paxeer-network/ui";

const PAXEER_NETWORK_ORDER_SOUND_ALERT_KEY = "paxeerNetwork_order_sound_alert";
const PAXEER_NETWORK_ORDER_SOUND_OPTION_KEY = "paxeerNetwork_order_sound_option";

const PAXEER_NETWORK_MWEB_ORDER_ENTRY_SIDE_MARKETS_LAYOUT =
  "paxeerNetwork_mweb_order_entry_side_markets_layout";

export type OrderPanelLayout = "left" | "right";

export type SettingScriptReturns = ReturnType<typeof useSettingScript>;

export const useSettingScript = () => {
  const { data } = useAccountInfo();
  const { wrongNetwork, disabledConnect } = useAppContext();
  const [update] = useMutation("/v1/client/maintenance_config");
  const [checked, setChecked] = useState(false);
  const { notification } = usePaxeerNetworkContext();
  const { themes, currentThemeId, setCurrentThemeId } = usePaxeerNetworkTheme();

  const soundOptions = notification?.orderFilled?.soundOptions;
  const hasSoundOptions = Boolean(soundOptions && soundOptions.length > 0);
  const defaultSoundValue =
    notification?.orderFilled?.defaultSoundValue ?? soundOptions?.[0]?.value;

  const [soundAlert, setSoundAlert] = useLocalStorage<boolean>(
    PAXEER_NETWORK_ORDER_SOUND_ALERT_KEY,
    notification?.orderFilled?.defaultOpen ?? false,
  );

  const [selectedSoundValue, setSelectedSoundValue] = useLocalStorage<
    string | null
  >(PAXEER_NETWORK_ORDER_SOUND_OPTION_KEY, defaultSoundValue ?? null);

  const [orderPanelLayout, setOrderPanelLayout] =
    useLocalStorage<OrderPanelLayout>(
      PAXEER_NETWORK_MWEB_ORDER_ENTRY_SIDE_MARKETS_LAYOUT,
      "right",
    );

  useEffect(() => {
    setChecked(data?.maintenance_cancel_orders || false);
  }, [data]);

  const updateCheckState = useDebouncedCallback((value: boolean) => {
    // if (value === data?.maintenance_cancel_orders) return;
    update({
      maintenance_cancel_order_flag: value,
    }).then((data) => {
      if (data.success) {
        toast.success(value ? "Opened" : "Closed");
      } else {
        setChecked(!value);
      }
    });
  }, 300);

  const setMaintainConfig = (maintenance_cancel_order_flag: boolean) => {
    setChecked(maintenance_cancel_order_flag);
    updateCheckState(maintenance_cancel_order_flag);
  };
  const { state } = useAccount();

  const canTouch =
    !wrongNetwork &&
    !disabledConnect &&
    (state.status === AccountStatusEnum.EnableTrading ||
      state.status === AccountStatusEnum.EnableTradingWithoutConnected);

  return {
    maintenance_cancel_orders: checked, //data?.maintenance_cancel_orders,
    setMaintainConfig,
    isSetting: false,
    canTouch,
    soundAlert,
    setSoundAlert,
    hasOrderFilledMedia: Boolean(notification?.orderFilled?.media),
    soundOptions,
    hasSoundOptions,
    selectedSoundValue,
    setSelectedSoundValue,
    orderPanelLayout,
    setOrderPanelLayout,
    themes,
    currentThemeId,
    setCurrentThemeId,
  };
};
