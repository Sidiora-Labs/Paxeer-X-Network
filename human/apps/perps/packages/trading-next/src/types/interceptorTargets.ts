/**
 * Module augmentation: maps interceptor target paths to their component props types.
 * Import from @paxeer-network/trading to enable typed props in
 * createInterceptor('Trading.DesktopLayout', ...), createInterceptor('Account.MobileAccountMenu', ...), etc.
 */
import type {} from "@paxeer-network/plugin-core";
import { SymbolInfoBarFullProps } from "@paxeer-network/markets";
import type { Props as OrderBookDesktopAsksProps } from "../components/desktop/orderBook/asks.desktop";
import type { Props as OrderBookDesktopBidsProps } from "../components/desktop/orderBook/bids.desktop";
import type { AccountState } from "../components/mobile/bottomNavBar/account/account.script";
import type { DesktopLayoutProps } from "../pages/trading/trading.ui.desktop";

declare module "@paxeer-network/plugin-core" {
  interface InterceptorTargetPropsMap {
    "Account.MobileAccountMenu": AccountState;
    "OrderBook.Desktop.Asks": OrderBookDesktopAsksProps;
    "OrderBook.Desktop.Bids": OrderBookDesktopBidsProps;
    "Trading.Layout.Desktop": DesktopLayoutProps;
    "Trading.SymbolInfoBar.Desktop": SymbolInfoBarFullProps;
  }
}
