import { FC } from "react";
import { useScreen } from "@paxeer-network/ui";
import { InjectableDesktopLayout } from "./trading.injectable";
import type { TradingState } from "./trading.script";
import { MobileLayout } from "./trading.ui.mobile";

/** Injectable so layout plugins can intercept and replace desktop layout via renderContent */

export const Trading: FC<TradingState> = (props) => {
  const { isMobile } = useScreen();

  if (isMobile) {
    return <MobileLayout {...props} />;
  }
  // className: oui-h-[calc(100vh_-_48px_-_29px_-_16px)]
  return <InjectableDesktopLayout className="oui-bg-base-10" {...props} />;
};
