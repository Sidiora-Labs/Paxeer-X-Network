import { FC } from "react";
import { cn, Flex } from "@paxeer-network/ui";
import { MainNavItem, NavItem } from "./navItem";

export type MainNavClassNames = {
  root?: string;
  navItem?: string;
  subMenu?: string;
};

export type MainNavItemsProps = {
  items?: MainNavItem[];
  current?: string[];
  classNames?: MainNavClassNames;
  onItemClick?: (item: MainNavItem[]) => void;
};

export const MainNavMenus: FC<MainNavItemsProps> = (props) => {
  const { items, classNames } = props;

  if (!Array.isArray(items) || items.length === 0) {
    return null;
  }

  return (
    <Flex className={cn("oui-gap-1", classNames?.root)}>
      {items?.map((item, index) => (
        <NavItem
          key={index}
          item={item}
          // active={item.href === props.current?.[0]}
          currentPath={props.current}
          onClick={props.onItemClick}
          classNames={classNames}
        />
      ))}
    </Flex>
  );
};
