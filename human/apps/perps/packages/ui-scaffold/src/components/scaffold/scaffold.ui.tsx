import React from "react";
import { isValidElement } from "react";
import { Box, cn, Grid, Text } from "@paxeer-network/ui";
import { FooterWidget } from "../footer";
import { MainNavWidget } from "../main/mainNav.widget";
import { NotificationWidget } from "../notification/notification.widget";
import { RestrictedInfoWidget } from "../restrictedInfo";
import { SideNavbarWidget } from "../sidebar";
import type { ScaffoldScriptReturn } from "./scaffold.script";
import type { ScaffoldProps } from "./scaffold.widget";

export const DefaultTradingViewTncLink = ({
  className,
}: {
  className?: string;
}) => {
  return (
    <a
      href="https://www.tradingview.com/"
      target="_blank"
      rel="noopener noreferrer"
      className={className ?? ""}
    >
      <Text intensity={54} size="2xs">
        Charts powered by TradingView
      </Text>
    </a>
  );
};

export type DesktopScaffoldProps = React.PropsWithChildren<
  ScaffoldProps & ScaffoldScriptReturn
>;

export const DesktopScaffold: React.FC<DesktopScaffoldProps> = (props) => {
  const {
    classNames,
    footerHeight,
    topNavbarRef,
    mainNavProps,
    topBar,
    announcementRef,
    // restrictedInfo,
    hasLeftSidebar,
    expand,
    leftSideProps,
    leftSidebar,
    footer,
    footerRef,
    sideBarCollaspedWidth,
    sideBarExpandWidth,
    footerProps,
    children,
  } = props;

  const trailing =
    typeof footerProps?.trailing !== "undefined" ? (
      footerProps?.trailing
    ) : (
      <DefaultTradingViewTncLink />
    );

  return (
    <div
      style={{
        height: `calc(100vh - ${footerHeight}px)`,
      }}
      className={cn(
        "oui-scaffold-root oui-font-semibold",
        // default text and background color
        "oui-bg-base-10 oui-text-base-contrast",
        "oui-flex oui-flex-col",
        "oui-custom-scrollbar oui-overflow-auto",
        classNames?.root,
      )}
    >
      {/* topNavbar */}
      <Box
        ref={topNavbarRef}
        className={cn(
          "oui-scaffold-topNavbar oui-bg-base-9",
          classNames?.topNavbar,
        )}
      >
        {topBar ?? <MainNavWidget {...mainNavProps} />}
      </Box>
      <div
        className={cn(
          "oui-scaffold-container",
          "oui-relative oui-h-full",
          // 1024px - 6px (scrollbar widt) = 1018px
          "oui-min-w-[1018px]",
          classNames?.container,
        )}
      >
        <Box px={2} ref={announcementRef}>
          <RestrictedInfoWidget
            className={cn(
              "oui-scaffold-restricted-info",
              "oui-relative oui-z-[1]",
              "oui-mt-2",
              "oui-bg-base-9",
              "oui-min-w-[994px]",
            )}
          />
          {/* <AnnouncementWidget
            className={"oui-mx-auto oui-mt-2"}
            hideTips={restrictedInfo?.restrictedOpen}
          /> */}
        </Box>
        {/*--------- body start ------ */}
        {!hasLeftSidebar ? (
          // ----------No leftSidebar layout start ---------
          <Box height="100%" className={cn(classNames?.content)}>
            {children}
          </Box>
        ) : (
          // ----------No leftSidebar layout end ---------
          // ---------- left & body layout start ---------
          <Grid
            className={cn(
              "oui-box-content oui-flex oui-transition-all xl:oui-grid",
              "oui-min-h-full oui-flex-1",
              classNames?.body,
            )}
            style={{
              gridTemplateColumns: `${
                expand
                  ? `${sideBarExpandWidth}px`
                  : `${sideBarCollaspedWidth}px`
              } 1fr`,
              // gridTemplateRows: "auto 1fr",
              // gridTemplateAreas: `"left main" "left main"`,
            }}
          >
            <div className={cn(classNames?.leftSidebar)}>
              {isValidElement<React.ReactElement>(leftSidebar) ? (
                leftSidebar
              ) : (
                <SideNavbarWidget {...leftSideProps} />
              )}
            </div>
            <Box
              width={"100%"}
              className={cn("oui-overflow-hidden", classNames?.content)}
            >
              {children}
            </Box>
          </Grid>
          // ---------- left & body layout end ---------
        )}
      </div>

      {/* footer */}
      <Box
        ref={footerRef}
        className={cn(
          "oui-scaffold-footer oui-w-full oui-bg-base-10",
          "oui-fixed oui-bottom-0 oui-z-50",
          "oui-border-t oui-border-line-12",
          classNames?.footer,
        )}
      >
        {footer || <FooterWidget {...footerProps} trailing={trailing} />}
      </Box>
      <NotificationWidget />
    </div>
  );
};
