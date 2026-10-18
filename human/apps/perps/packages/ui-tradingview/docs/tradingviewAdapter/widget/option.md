# option

## Overview

Builds TradingTerminal widget options: merges default enabled/disabled features with mode-based disabled list (BASIC, ADVANCED, MOBILE) and optional external enabled_features/disabled_features. Sets broker_config (supportStopLimitOrders, supportReversePosition) and auto_save_delay.

## getDisabledFeatures(mode)

Returns array of disabled feature IDs; adds left_toolbar, timeframes_toolbar, buy_sell_buttons etc. for BASIC/ADVANCED/MOBILE.

## getOptions(options, mode, externalEnabledFeatures?, externalDisabledFeatures?)

Merges default enabled (hide_left_toolbar_by_default, order_panel_close_button, iframe_loading_compatibility_mode) with external; merges getDisabledFeatures(mode) with external disabled. Returns options with disabled_features, enabled_features, auto_save_delay, broker_config.
