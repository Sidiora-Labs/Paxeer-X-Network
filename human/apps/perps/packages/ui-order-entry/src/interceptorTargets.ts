/**
 * Module augmentation: maps OrderEntry interceptor target paths to props types.
 * Import from @paxeer-network/ui-order-entry to enable typed props in
 * createInterceptor("Trading.OrderEntry.*", (Original, props, api) => ...).
 */
import type {
  MobileTypeSelectProps,
  OrderEntryBuySellSwitchProps,
  OrderEntryAvailableProps,
  OrderEntryBodyProps,
  OrderEntryQuantitySliderProps,
  OrderEntrySubmitSectionProps,
  OrderEntryTypeTabsProps,
  OrderTypeAdvancedSelectProps,
} from "./components/orderEntry.injectabled";
import type { OrderEntryProps } from "./orderEntry.ui";

declare module "@paxeer-network/plugin-core" {
  interface InterceptorTargetPropsMap {
    OrderEntry: OrderEntryProps;
    "Trading.OrderEntry.AdvancedSelect": OrderTypeAdvancedSelectProps;
    "Trading.OrderEntry.Available": OrderEntryAvailableProps;
    "Trading.OrderEntry.Body": OrderEntryBodyProps;
    "Trading.OrderEntry.BuySellSwitch": OrderEntryBuySellSwitchProps;
    "Trading.OrderEntry.MobileTypeSelect": MobileTypeSelectProps;
    "Trading.OrderEntry.QuantitySlider": OrderEntryQuantitySliderProps;
    "Trading.OrderEntry.SubmitSection": OrderEntrySubmitSectionProps;
    "Trading.OrderEntry.TypeTabs": OrderEntryTypeTabsProps;
  }
}
