import React, { PropsWithChildren, ReactNode } from "react";
import { LeftNavProps } from "../leftNav/leftNav.type";
import { MainNavClassNames } from "./mainMenus/mainNavMenus.ui";
import { MainNavItem } from "./mainMenus/navItem";
import { CampaignPositionEnum, useMainNavScript } from "./mainNav.script";
import { MainNav } from "./mainNav.ui";

export type MainNavWidgetProps = {
  leading?: ReactNode;
  trailing?: ReactNode;
  logo?: {
    src: string;
    alt: string;
  };
  mainMenus?: MainNavItem[];
  /** @deprecated use mainMenus instead */
  campaigns?: MainNavItem;
  /** @deprecated use mainMenus instead */
  campaignPosition?: CampaignPositionEnum;
  /**
   * initial menu path, if it has submenus, use array
   * @type string | string[]
   */
  initialMenu?: string | string[];

  onItemClick?: (options: {
    href: string;
    name: string;
    scope?: string;
  }) => void;

  /** only works on mobile */
  leftNav?: LeftNavProps;
  customLeftNav?: ReactNode;
  className?: string;
  classNames?: {
    root?: string;
    mainNav?: MainNavClassNames;
    // subNav?: string;
    logo?: string;
    account?: string;
    chains?: string;
    campaignButton?: string;
  };

  /** custom render main nav */
  customRender?: (components: {
    /** Logo or title component (desktop & mobile) */
    title?: ReactNode;
    /** Language selection component (desktop & mobile) */
    languageSwitcher?: ReactNode;
    /** Sub-account component (desktop & mobile) */
    subAccount?: ReactNode;
    /** Device linking component (desktop & mobile) */
    linkDevice?: ReactNode;
    /** Chain selection menu (desktop & mobile) */
    chainMenu?: ReactNode;
    /** Wallet connection component (desktop & mobile) */
    walletConnect?: ReactNode;

    notify?: ReactNode;

    /** Main navigation menu (desktop & mobile) */
    mainNav?: ReactNode;
    /** Account summary component (desktop only) */
    accountSummary?: ReactNode;

    /** Left navigation component (mobile only) */
    leftNav?: ReactNode;
    /** QR code scanner component (mobile only) */
    scanQRCode?: ReactNode;
  }) => ReactNode;
};

export const MainNavWidget: React.FC<PropsWithChildren<MainNavWidgetProps>> = (
  props,
) => {
  const { children, classNames, ...rest } = props;
  const state = useMainNavScript(rest);
  return (
    <MainNav classNames={classNames} {...state}>
      {children}
    </MainNav>
  );
};
