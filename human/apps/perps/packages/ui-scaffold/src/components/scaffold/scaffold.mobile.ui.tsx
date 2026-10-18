import React from "react";
import { Box, cn } from "@paxeer-network/ui";
import { MainNavMobile } from "../main/mainNav.ui.mobile";
import { NotificationWidget } from "../notification/notification.widget";
import type { ScaffoldScriptReturn } from "./scaffold.script";
import type { ScaffoldProps } from "./scaffold.widget";

const LazyRestrictedInfoWidget = React.lazy(() =>
  import("../restrictedInfo").then((mod) => {
    return { default: mod.RestrictedInfoWidget };
  }),
);

const LazyBottomNav = React.lazy(() =>
  import("../bottomNav").then((mod) => {
    return { default: mod.BottomNav };
  }),
);

export const MobileScaffold: React.FC<
  React.PropsWithChildren<ScaffoldScriptReturn & ScaffoldProps>
> = (props) => {
  const {
    classNames,
    topNavbarRef,
    bottomNavHeight,
    topBar,
    mainNavProps,
    routerAdapter,
    bottomNavRef,
    bottomNavProps,
    bottomNav,
    children,
  } = props;

  return (
    <div
      style={{
        paddingBottom: `calc(${bottomNavHeight}px + 12px + env(safe-area-inset-bottom))`,
      }}
      className={cn(
        "oui-scaffold-root oui-w-full oui-bg-base-10 oui-pt-2",
        classNames?.root,
      )}
    >
      <header
        ref={topNavbarRef}
        className={cn(
          "oui-scaffold-topNavbar",
          "oui-sticky oui-top-0 oui-z-10 oui-w-full oui-bg-base-10",
          classNames?.topNavbar,
        )}
      >
        {topBar ?? (
          <MainNavMobile {...mainNavProps} routerAdapter={routerAdapter} />
        )}
      </header>

      <Box
        className={cn(
          "oui-scaffold-container oui-overflow-hidden",
          // "oui-relative",
          // "oui-custom-scrollbar oui-overflow-y-auto",
          classNames?.container,
        )}
      >
        <React.Suspense fallback={null}>
          <LazyRestrictedInfoWidget className="oui-mx-1 oui-mb-1 oui-bg-base-6" />
        </React.Suspense>

        <Box
          height="100%"
          width="100%"
          className={cn("oui-scaffold-content", classNames?.content)}
        >
          {children}
        </Box>
      </Box>
      <footer
        ref={bottomNavRef}
        className={cn(
          "oui-scaffold-bottomNav",
          "oui-fixed oui-bottom-0 oui-z-10",
          "oui-w-full oui-bg-base-9",
          // only effective on real device
          "oui-pb-[calc(env(safe-area-inset-bottom))]",
          classNames?.bottomNav,
        )}
      >
        {bottomNav ?? (
          <React.Suspense fallback={null}>
            <LazyBottomNav
              mainMenus={bottomNavProps?.mainMenus}
              current={bottomNavProps?.current || mainNavProps?.initialMenu}
              onRouteChange={routerAdapter?.onRouteChange}
            />
          </React.Suspense>
        )}
      </footer>

      <NotificationWidget />
    </div>
  );
};
