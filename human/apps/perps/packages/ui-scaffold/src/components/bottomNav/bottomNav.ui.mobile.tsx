import React, { useMemo } from "react";
import { type RouterAdapter } from "@paxeer-network/types";
import { Flex, Text } from "@paxeer-network/ui";
import { BottomNavItem } from "./bottomNav.widget";

export type BottomNavProps = {
  mainMenus?: BottomNavItem[];
  current?: string | string[];
  onRouteChange?: RouterAdapter["onRouteChange"];
};

export const BottomNav: React.FC<BottomNavProps> = (props) => {
  const { mainMenus, current, onRouteChange } = props;

  const isSubmenu = useMemo(() => {
    return mainMenus?.every((menu) => menu.href !== current);
  }, [mainMenus, current]);

  const menus = useMemo(() => {
    return mainMenus?.map((menu) => {
      const isActive = current === menu.href;
      const isExternalLink =
        menu.href.startsWith("http://") || menu.href.startsWith("https://");
      // const isTrading = menu.href === "/" || /trading/i.test(menu.name);
      return (
        <Flex
          key={menu.name}
          direction={"column"}
          itemAlign={"center"}
          justify={"center"}
          className="oui-flex-1"
          onClick={() => {
            if (isExternalLink) {
              window.location.href = menu.href;
            } else {
              onRouteChange?.({ href: menu.href, name: menu.name });
            }
          }}
        >
          {/* TODO: add new trading menu when ai is ready */}
          {/* {isTrading ? (
            <BottomNavTradingMenu
              label={menu.name}
              active={!!isActive}
              activeIcon={menu.activeIcon}
              inactiveIcon={menu.inactiveIcon}
              onNavigate={() =>
                onRouteChange?.({ href: menu.href, name: menu.name })
              }
            />
          ) : (
            <Text>{isActive ? menu.activeIcon : menu.inactiveIcon}</Text>
          )} */}
          <Text>{isActive ? menu.activeIcon : menu.inactiveIcon}</Text>
          <Text size="2xs" intensity={isActive ? 98 : 36}>
            {menu.name}
          </Text>
        </Flex>
      );
    });
  }, [mainMenus, current, onRouteChange]);

  if (isSubmenu || !mainMenus) {
    return null;
  }

  return (
    <Flex
      width={"100%"}
      height={64}
      px={3}
      itemAlign={"center"}
      justify={"between"}
      className="oui-border-t oui-border-line-4 oui-bg-base-9"
    >
      {menus}
    </Flex>
  );
};
