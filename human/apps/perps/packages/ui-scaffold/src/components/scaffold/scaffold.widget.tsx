import React, { PropsWithChildren, ReactNode } from "react";
import { type RouterAdapter } from "@paxeer-network/types";
import { BottomNavProps } from "../bottomNav/bottomNav.widget";
import { FooterProps } from "../footer";
import { LeftNavProps } from "../leftNav";
import { MainNavWidgetProps } from "../main/mainNav.widget";
import { SideBarProps } from "../sidebar";
import { MobileScaffold } from "./scaffold.mobile.ui";
import { useScaffoldScript } from "./scaffold.script";
import { DesktopScaffold } from "./scaffold.ui";
import { ScaffoldProvider } from "./scaffoldProvider";

export type ScaffoldProps = {
  /**
   * custom left sidebar component, only works on desktop
   * if provided, the layout will use this component over the default sidebar component
   */
  leftSidebar?: ReactNode;
  /**
   * custom left sidebar props, only works on desktop
   */
  leftSideProps?: SideBarProps;
  /**
   * custom left nav props, only works on mobile
   */
  leftNavProps?: LeftNavProps;
  /**
   * custom top bar component
   */
  topBar?: ReactNode;
  /**
   * custom top nav props
   */
  mainNavProps?: MainNavWidgetProps;
  /**
   * custom bottom nav component, only works on mobile
   */
  bottomNav?: ReactNode;
  /**
   * custom bottom nav component, only works on mobile
   */
  bottomNavProps?: BottomNavProps;
  /**
   * custom footer component, only works on desktop
   */
  footer?: ReactNode;
  /**
   * custom footer props, only works on desktop
   */
  footerProps?: FooterProps;
  routerAdapter?: RouterAdapter;
  classNames?: {
    // root = topNavbar + container + footer
    root?: string;
    container?: string;
    content?: string;
    // body = leftSidebar + content
    body?: string;
    leftSidebar?: string;
    topNavbar?: string;
    footer?: string;
    bottomNav?: string;
  };
};

export const Scaffold: React.FC<PropsWithChildren<ScaffoldProps>> = (props) => {
  const state = useScaffoldScript(props);

  const renderContent = () => {
    if (state.isMobile) {
      return (
        <MobileScaffold {...props} {...state}>
          {props.children}
        </MobileScaffold>
      );
    }

    return (
      <DesktopScaffold {...props} {...state}>
        {props.children}
      </DesktopScaffold>
    );
  };

  return (
    <ScaffoldProvider
      routerAdapter={props.routerAdapter}
      expanded={state.expand}
      setExpand={state.setExpand}
      topNavbarHeight={state.topNavbarHeight}
      footerHeight={state.footerHeight}
      announcementHeight={state.announcementHeight}
    >
      {renderContent()}
    </ScaffoldProvider>
  );
};
