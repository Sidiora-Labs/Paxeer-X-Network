/**
 * Side-effect: augment InterceptorTargetPropsMap for typed interceptor props.
 */
import "./interceptorTargets";

export {
  AccountMenuWidget,
  type AccountMenuProps,
} from "./components/accountMenu";
export {
  MainNavWidget,
  type MainNavWidgetProps,
} from "./components/main/mainNav.widget";
export { MainNavMobile } from "./components/main/mainNav.ui.mobile";
export { type MainNavItem } from "./components/main/mainMenus/navItem";
export { type MainNavItemsProps } from "./components/main/mainMenus/mainNavMenus.ui";
export {
  BottomNavWidget,
  BottomNav,
  type BottomNavProps,
} from "./components/bottomNav";
export { AccountSummaryWidget } from "./components/accountSummary";
export { ChainMenuWidget, ChainMenu } from "./components/chainMenu";
export { SideNavbarWidget, SideBar } from "./components/sidebar";
export { CampaignPositionEnum } from "./components/main/mainNav.script";

export * from "./components/subAccount";

export * from "./components/footer";
export * from "./components/maintenanceTips";

export * from "./components/restrictedInfo";

export type { SideBarProps, SideMenuItem } from "./components/sidebar";

// ----------------- Main layout component -----------------
export {
  Scaffold,
  type ScaffoldProps,
  useScaffoldContext,
  ScaffoldContext,
} from "./components/scaffold";

export { MainLogo } from "./components/main/mainLogo";
export * from "./components/languageSwitcher";
export * from "./components/scanQRCode";
export * from "./components/leftNav";

export type {RouterAdapter} from "@paxeer-network/types";
