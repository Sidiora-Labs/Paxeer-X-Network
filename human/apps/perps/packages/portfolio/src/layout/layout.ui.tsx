import { FC, PropsWithChildren } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import type { RouterAdapter } from "@paxeer-network/types";
import { cn } from "@paxeer-network/ui";
import {
  ScaffoldProps,
  Scaffold,
  SideBar,
  SideBarProps,
  useScaffoldContext,
} from "@paxeer-network/ui-scaffold";

export type PortfolioLayoutProps = ScaffoldProps & {
  hideSideBar?: boolean;
  items?: SideBarProps["items"];
};

export const PortfolioLayout: FC<PropsWithChildren<PortfolioLayoutProps>> = (
  props,
) => {
  const { children, leftSideProps, classNames, ...rest } = props;

  return (
    <Scaffold
      leftSidebar={
        props.hideSideBar ? null : (
          <LeftSidebar
            current={props.routerAdapter?.currentPath}
            routerAdapter={props.routerAdapter}
            items={props.items}
            {...leftSideProps}
          />
        )
      }
      routerAdapter={props.routerAdapter}
      classNames={{
        ...classNames,
        content: cn("oui-my-6 oui-px-3", classNames?.content),
        topNavbar: cn("oui-bg-base-9", classNames?.topNavbar),
        leftSidebar: cn(
          "oui-rounded-xl oui-bg-base-9",
          "oui-m-3 oui-p-4",
          "oui-border oui-border-line",
          classNames?.leftSidebar,
        ),
      }}
      {...rest}
    >
      {children}
    </Scaffold>
  );
};

type LeftSidebarProps = SideBarProps & {
  routerAdapter?: RouterAdapter;
};

const LeftSidebar: FC<LeftSidebarProps> = (props) => {
  const { expanded, setExpand } = useScaffoldContext();
  const { t } = useTranslation();
  return (
    <SideBar
      title={t("common.portfolio")}
      {...props}
      open={expanded}
      onOpenChange={(open) => setExpand(open)}
      onItemSelect={(e) => {
        props.onItemSelect?.(e);
        props.routerAdapter?.onRouteChange?.({
          href: e.href || "",
          name: e.name,
        });
      }}
    />
  );
};
