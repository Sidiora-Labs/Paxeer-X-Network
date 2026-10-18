import { ChartMode, TradingTerminalWidgetOptions } from "../type";

const getDisabledFeatures = (mode: ChartMode) => {
  let disabledFeatures = [
    "header_symbol_search",
    "volume_force_overlay",
    "trading_account_manager",
    "drawing_templates",
    "open_account_manager",
    "right_toolbar",
    "support_multicharts",
    "header_layouttoggle",
    "order_panel",
    "order_info",
    "trading_notifications",
    "display_market_status",
    "broker_button",
    "add_to_watchlist",
    "chart_crosshair_menu",
    "header_fullscreen_button",
    "header_widget",
  ];

  if (mode === ChartMode.MOBILE) {
    disabledFeatures = [
      ...disabledFeatures,
      "left_toolbar",
      "timeframes_toolbar",
      "go_to_date",
      "timezone_menu",
      // add volume back
      "create_volume_indicator_by_default",
      "buy_sell_buttons",
    ];
  }
  if (mode === ChartMode.BASIC) {
    disabledFeatures = [
      ...disabledFeatures,
      "header_widget",
      "left_toolbar",
      "timeframes_toolbar",
      "buy_sell_buttons",
    ];
  } else if (mode === ChartMode.ADVANCED) {
    disabledFeatures = [
      ...disabledFeatures,
      "left_toolbar",
      "timeframes_toolbar",
      "buy_sell_buttons",
    ];
  }

  return disabledFeatures;
};

/**
 * Merges options and returns TradingTerminal widget configuration
 * @param options - Base widget options
 * @param mode - Chart mode (BASIC, ADVANCED, MOBILE)
 * @param externalEnabledFeatures - Additional enabled features from external props
 * @param externalDisabledFeatures - Additional disabled features from external props
 */
export default function getOptions(
  options: any,
  mode: any,
  externalEnabledFeatures?: string[],
  externalDisabledFeatures?: string[],
): TradingTerminalWidgetOptions {
  // Default internal enabled features
  const defaultEnabledFeatures = [
    "hide_left_toolbar_by_default",
    "order_panel_close_button",
    "iframe_loading_compatibility_mode",
  ];

  // Merge enabled features: combine defaults with external, remove duplicates
  const mergedEnabledFeatures = Array.from(
    new Set([...defaultEnabledFeatures, ...(externalEnabledFeatures || [])]),
  );

  // Get default disabled features based on mode
  const defaultDisabledFeatures = getDisabledFeatures(mode);

  // Merge disabled features: combine defaults with external, remove duplicates
  const mergedDisabledFeatures = Array.from(
    new Set([...defaultDisabledFeatures, ...(externalDisabledFeatures || [])]),
  );

  return {
    ...options,
    disabled_features: mergedDisabledFeatures,
    enabled_features: mergedEnabledFeatures,
    auto_save_delay: 0.1,
    broker_config: {
      configFlags: {
        supportStopLimitOrders: true,
        supportReversePosition: false,
      },
    },
  };
}
