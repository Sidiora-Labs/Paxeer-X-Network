import { FC, PropsWithChildren } from "react";
import { Flex, Box } from "@paxeer-network/ui";
import {
  MainNavMobile,
  BottomNav,
  type ScaffoldProps,
} from "@paxeer-network/ui-scaffold";
import { LayoutProvider } from "./context";
import { usePortfolioLayoutScriptType } from "./layout.script";

export const PortfolioLayoutMobile: FC<
  PropsWithChildren<
    ScaffoldProps & usePortfolioLayoutScriptType & { current?: string }
  >
> = (props) => {
  const hasBottomNav = props.bottomNavProps?.mainMenus?.some(
    (menu) => menu.href === props.current,
  );
  const contentBottomPadding = hasBottomNav
    ? "calc(64px + 12px + env(safe-area-inset-bottom))"
    : "calc(12px + env(safe-area-inset-bottom))";

  return (
    <LayoutProvider {...props}>
      <Flex
        direction={"column"}
        width={"100%"}
        height={"100%"}
        className="oui-bg-base-10"
      >
        <header className="oui-scaffold-topNavbar oui-sticky oui-top-0 oui-z-10 oui-w-full oui-bg-base-10">
          <MainNavMobile
            {...props.mainNavProps}
            current={props?.current}
            subItems={props?.items}
            routerAdapter={props.routerAdapter}
          />
        </header>
        <Box
          className="oui-scaffold-container oui-min-h-[calc(100vh-44px)] oui-w-full"
          style={{
            // Use dvh when available, with the className vh value as a fallback for older WebViews.
            minHeight: "calc(100dvh - 44px)",
            paddingBottom: contentBottomPadding,
          }}
        >
          {props.children}
        </Box>
        <footer className="oui-scaffold-bottomNav oui-fixed oui-bottom-0 oui-z-10 oui-w-full oui-bg-base-9 oui-pb-[calc(env(safe-area-inset-bottom))]">
          <BottomNav
            mainMenus={props.bottomNavProps?.mainMenus}
            current={props?.current}
            onRouteChange={props.routerAdapter?.onRouteChange}
          />
        </footer>
      </Flex>
    </LayoutProvider>
  );
};
