import { ReactNode } from "react";

export type LeftNavProps = {
  /** custom leading */
  leading?: ReactNode;
  menus?: LeftNavItem[];
  twitterUrl?: string;
  telegramUrl?: string;
  discordUrl?: string;
  duneUrl?: string;
  /**
   * @deprecated Deprecated. This prop will no longer affect the rendered UI
   * in the leftNav drawer footer. Use `customFooter` instead.
   */
  feedbackUrl?: string;
  customFooter?: ReactNode;
  customLeftNav?: ReactNode;
};

export type LeftNavItem = {
  name: string;
  href: string;
  target?: string;
  icon?: ReactNode;
  trailing?: ReactNode;
  customRender?: (option: {
    name: string;
    href: string;
    isActive?: boolean;
  }) => React.ReactNode;
  /**
   * if true, this item will only be shown in the main account
   * @default false
   **/
  onlyInMainAccount?: boolean;
  /**
   * Optional secondary style; secondary items render smaller and gray.
   * Secondary items are grouped below primary items with a divider in between.
   */
  isSecondary?: boolean;
};
