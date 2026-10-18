import React, { ReactNode } from "react";
import { type RouterAdapter } from "@paxeer-network/types";
import { useScreen } from "@paxeer-network/ui";
import { BottomNav } from "./bottomNav.ui.mobile";

export type BottomNavItem = {
  name: string;
  href: string;
  activeIcon: ReactNode;
  inactiveIcon: ReactNode;
};

export type BottomNavProps = {
  mainMenus?: BottomNavItem[];
  current?: string;
  onRouteChange?: RouterAdapter["onRouteChange"];
};

export const BottomNavWidget: React.FC<BottomNavProps> = (props) => {
  const { isMobile } = useScreen();
  const { mainMenus, ...rest } = props;
  return isMobile ? <BottomNav mainMenus={mainMenus} {...rest} /> : null;
};
