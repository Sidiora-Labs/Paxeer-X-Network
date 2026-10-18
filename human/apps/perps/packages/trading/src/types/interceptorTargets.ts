/**
 * Module augmentation: maps interceptor target paths to their component props types.
 * Import from @paxeer-network/trading to enable typed props in
 * createInterceptor('Trading.Layout.Desktop', ...), createInterceptor('Account.MobileAccountMenu', ...), etc.
 */
import type {} from "@paxeer-network/plugin-core";
import type { SymbolInfoBarFullProps } from "@paxeer-network/markets";
import type { DataListDesktopTabsProps } from "../components/desktop/dataList/dataList.injectable";
import type { Props as OrderBookDesktopAsksProps } from "../components/desktop/orderBook/asks.desktop";
import type { Props as OrderBookDesktopBidsProps } from "../components/desktop/orderBook/bids.desktop";
import type { AccountState } from "../components/mobile/bottomNavBar/account/account.script";
import type { DataListMobileTabsProps } from "../components/mobile/dataList/dataList.injectable";
import type { DesktopLayoutProps } from "../pages/trading/trading.ui.desktop";
import type { MobileLayoutProps } from "../pages/trading/trading.ui.mobile";

declare module "@paxeer-network/plugin-core" {
  interface InterceptorTargetPropsMap {
    "Account.MobileAccountMenu": AccountState;
    "OrderBook.Desktop.Asks": OrderBookDesktopAsksProps;
    "OrderBook.Desktop.Bids": OrderBookDesktopBidsProps;
    "Trading.DataList.Desktop.Tabs": DataListDesktopTabsProps;
    "Trading.DataList.Mobile.Tabs": DataListMobileTabsProps;
    "Trading.Layout.Desktop": DesktopLayoutProps;
    "Trading.Layout.Mobile": MobileLayoutProps;
    "Trading.SymbolInfoBar.Desktop": SymbolInfoBarFullProps;
  }
}
