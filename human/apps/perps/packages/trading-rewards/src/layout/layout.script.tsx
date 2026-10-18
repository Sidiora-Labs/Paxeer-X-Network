import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { useScreen } from "@paxeer-network/ui";
import type { SideBarProps } from "@paxeer-network/ui-scaffold";

export enum TradingRewardsLeftSidebarPath {
  Trading = "/rewards/trading",
  Affiliate = "/rewards/affiliate",
}

export const useTradingRewardsLayoutScript = (props: {
  current?: string;
}): SideBarProps & {
  hideSideBar: boolean;
} => {
  const { t } = useTranslation();
  const [current, setCurrent] = useState(props.current || "/rewards/affiliate");

  useEffect(() => {
    if (props.current) setCurrent(props.current);
  }, [props.current]);

  const items = useMemo(() => {
    return [
      {
        name: t("common.trading"),
        href: TradingRewardsLeftSidebarPath.Trading,
        icon: (
          <svg
            width="18"
            height="18"
            viewBox="0 0 18 18"
            fill="none"
            xmlns="http://www.w3.org/2000/svg"
          >
            <path
              d="M4.883 1.5c-.166.01-.393.076-.515.173a2 2 0 0 0-.18.173L.918 6.553a.86.86 0 0 0-.168.475c0 .339.232.543.232.543l7.374 8.633c.005.006.269.301.644.296s.64-.29.644-.296l7.402-8.663s.204-.269.204-.513-.196-.517-.196-.517L13.82 1.848c-.023-.032-.076-.021-.103-.049-.033-.038-.037-.091-.077-.124-.123-.097-.267-.106-.413-.123-.04-.005-.063-.049-.103-.049h-.078zm1.745 1.582h4.744L9 5.795zm-1.675.542 2.294 2.615H3.122zm8.094 0 1.831 2.615h-4.125zM3.328 7.818h4.847v5.672zm6.497 0h4.847c-1.151 1.345-3.422 4.007-4.847 5.672z"
              // fill="url(#a)"
              className="oui-fill-current group-data-[actived=true]:oui-fill-[url(#side-menu-gradient)]"
            />
            <defs>
              <linearGradient
                id="a"
                x1="17.25"
                y1="9"
                x2=".75"
                y2="9"
                gradientUnits="userSpaceOnUse"
              >
                <stop stopColor="rgb(var(--oui-gradient-brand-end))" />
                <stop
                  offset="1"
                  stopColor="rgb(var(--oui-gradient-brand-start))"
                />
              </linearGradient>
            </defs>
          </svg>
        ),
      },
      {
        name: t("common.affiliate"),
        href: TradingRewardsLeftSidebarPath.Affiliate,
        icon: (
          <svg
            width="18"
            height="18"
            viewBox="0 0 18 18"
            fill="none"
            xmlns="http://www.w3.org/2000/svg"
          >
            <path
              fillRule="evenodd"
              clipRule="evenodd"
              d="M5.625 4.95c0-.746.604-1.35 1.35-1.35h4.05c.746 0 1.35.604 1.35 1.35v4.62L9 12.195 5.625 9.57zm8.624 3.163-.524.407v-.96l.074.05c.193.128.346.302.45.502m-4.42 5.148L14.4 9.705v3.345a1.35 1.35 0 0 1-1.35 1.35h-8.1a1.35 1.35 0 0 1-1.35-1.35V9.705l4.571 3.556c.488.379 1.17.379 1.658 0M4.275 8.52l-.524-.407c.104-.2.257-.374.45-.503l.074-.049zm0-2.581V4.95a2.7 2.7 0 0 1 2.7-2.7h4.05a2.7 2.7 0 0 1 2.7 2.7v.989l.823.548a2.7 2.7 0 0 1 1.202 2.247v4.316a2.7 2.7 0 0 1-2.7 2.7h-8.1a2.7 2.7 0 0 1-2.7-2.7V8.734a2.7 2.7 0 0 1 1.202-2.247zM9 6.089l.111-.118.005-.005a1.08 1.08 0 0 1 1.58.004 1.223 1.223 0 0 1 0 1.668l-1.515 1.62-.004.004c-.1.102-.26.1-.358-.004l-1.516-1.62a1.223 1.223 0 0 1 0-1.668l.004-.004a1.08 1.08 0 0 1 1.58.004z"
              className="oui-fill-current group-data-[actived=true]:oui-fill-[url(#side-menu-gradient)]"
            />
          </svg>
        ),
      },
    ];
  }, [t]);

  const { isMobile: hideSideBar } = useScreen();

  return {
    items,
    current,
    hideSideBar,
    // open: sideOpen,
    // onOpenChange(open) {
    //   setSideOpen(open);
    // },
    onItemSelect: (item) => {
      // @ts-ignore
      setCurrent(item.href);
    },
  };
};
