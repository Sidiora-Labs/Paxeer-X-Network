import { injectable } from "@paxeer-network/ui";
import { DesktopLayout } from "./trading.ui.desktop";
import { MobileLayout } from "./trading.ui.mobile";

export const InjectableDesktopLayout = injectable(
  DesktopLayout,
  "Trading.Layout.Desktop",
);

export const InjectableMobileLayout = injectable(
  MobileLayout,
  "Trading.Layout.Mobile",
);
