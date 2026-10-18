import React, { PropsWithChildren } from "react";
import omit from "ramda/es/omit";
import { cn } from "@paxeer-network/ui";
import { Scaffold, SideBarProps } from "@paxeer-network/ui-scaffold";
import { ScaffoldProps } from "@paxeer-network/ui-scaffold";

export type TradingRewardsLayoutProps = ScaffoldProps & {
  hideSideBar?: boolean;
  items?: SideBarProps["items"];
};

export const TradingRewardsLayout: React.FC<
  PropsWithChildren<TradingRewardsLayoutProps>
> = (props) => {
  const { children, classNames, ...rest } = props;
  return (
    <Scaffold
      leftSidebar={null}
      routerAdapter={props.routerAdapter}
      classNames={{
        ...classNames,
        content: classNames?.content,
        topNavbar: cn("oui-bg-base-9", classNames?.topNavbar),
        leftSidebar: cn(
          "oui-rounded-xl oui-bg-base-9",
          "oui-m-3 oui-p-4",
          "oui-border oui-border-line",
          classNames?.leftSidebar,
        ),
      }}
      {...omit(["leftSideProps"], rest)}
    >
      {children}
    </Scaffold>
  );
};

// type LeftSidebarProps = SideBarProps & {
//   routerAdapter?: RouterAdapter;
// };

// const LeftSidebar: React.FC<LeftSidebarProps> = (props) => {
//   const { t } = useTranslation();
//   const { expanded, setExpand } = useScaffoldContext();
//   return (
//     <SideBar
//       title={t("tradingRewards.rewards")}
//       {...props}
//       open={expanded}
//       onOpenChange={(open) => setExpand(open)}
//       onItemSelect={(a) => {
//         props.onItemSelect?.(a);
//         props.routerAdapter?.onRouteChange?.({
//           href: a.href || "",
//           name: a.name,
//         });
//       }}
//     />
//   );
// };
