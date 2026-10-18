import React, {
  cloneElement,
  FC,
  HTMLAttributeAnchorTarget,
  PropsWithChildren,
  ReactElement,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { useAccount, useLocalStorage } from "@paxeer-network/hooks";
import { ChevronDownIcon, PopoverContent, Tooltip } from "@paxeer-network/ui";
import { Flex } from "@paxeer-network/ui";
import { Box, cn, PopoverAnchor, PopoverRoot, Text } from "@paxeer-network/ui";

const ActiveIcon: React.FC = () => (
  <svg
    width={12}
    height={12}
    viewBox="0 0 12 12"
    fill="none"
    xmlns="http://www.w3.org/2000/svg"
    focusable={false}
  >
    <path
      d="M2.913 4.515a.5.5 0 0 0-.328.202.51.51 0 0 0 .14.701L5.722 7.41a.51.51 0 0 0 .562 0l2.995-1.992a.51.51 0 0 0 .14-.7.51.51 0 0 0-.701-.14L6.002 6.382 3.287 4.577a.5.5 0 0 0-.374-.062"
      fill="url(#mainNavDropDownIcon)"
    />
    <defs>
      <linearGradient
        id="mainNavDropDownIcon"
        x1="9.502"
        y1="5.994"
        x2="2.502"
        y2="5.994"
        gradientUnits="userSpaceOnUse"
      >
        <stop stopColor="rgb(var(--oui-gradient-brand-end))" />
        <stop stopColor="rgb(var(--oui-gradient-brand-start))" offset={1} />
      </linearGradient>
    </defs>
  </svg>
);

export type MainNavItem = {
  id?: string;
  testid?: string;
  name: string;
  label?: string;
  href: string;
  activeHrefs?: string[];
  target?: HTMLAttributeAnchorTarget;
  icon?: string | React.ReactElement;
  activeIcon?: string | React.ReactElement;
  tag?: string;
  description?: string;
  disabled?: boolean;
  children?: MainNavItem[];
  className?: string;
  asChild?: boolean;

  /**
   * if true, the item will be shown as a submenu in mobile
   */
  isSubMenuInMobile?: boolean;
  /**
   * if provided, the item is a submenu in mobile, and this will be the a back nav
   */
  subMenuBackNav?: {
    name: string;
    href: string;
  };
  /**
   * if true, the item will be shown as a home page in mobile
   */
  isHomePageInMobile?: boolean;
  customRender?: (option: {
    name: string;
    href: string;
    isActive?: boolean;
  }) => React.ReactNode;
  /**
   * Custom submenu renderer - provides full control over submenu content
   * Renders as a free-form component without any predefined structure
   * @returns React node to render as submenu content
   */
  customSubMenuRender?: () => React.ReactNode;
  /**
   * if true, this item will only be shown in the main account
   * @default false
   **/
  onlyInMainAccount?: boolean;
  /** @deprecated */
  tooltipConfig?: {
    /**
     * if true, the tooltip will be shown on first visit
     */
    showOnFirstVisit?: boolean;
    /**
     * the text to show in the tooltip
     */
    text?: string;
  };
};

const isObject = (value: any): value is object => {
  return !!value && value.constructor === Object;
};

const PAXEER_NETWORK_NAV_BUTTON_TOOLTIP_OPEN = "PAXEER_NETWORK_NAV_BUTTON_TOOLTIP_OPEN";

export const NavItem: FC<
  Omit<React.HTMLAttributes<HTMLButtonElement>, "onClick"> & {
    item: MainNavItem;
    onClick?: (item: MainNavItem[]) => void;
    // active?: boolean;
    currentPath?: string[];
    classNames?: {
      navItem?: string;
      subMenu?: string;
    };
  }
> = (props) => {
  const { classNames, currentPath, item, onClick, ...buttonProps } = props;

  const { customRender, tooltipConfig, onlyInMainAccount } = item;

  const { isMainAccount } = useAccount();

  const [showButtonTooltip, setShowButtonTooltip] = useLocalStorage(
    PAXEER_NETWORK_NAV_BUTTON_TOOLTIP_OPEN,
    true,
  );

  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    if (showButtonTooltip) {
      timerRef.current = setTimeout(() => {
        setShowButtonTooltip(false);
      }, 8000);
    }
    return () => {
      if (timerRef.current) {
        clearTimeout(timerRef.current);
      }
    };
  }, [showButtonTooltip]);

  const isActive = useMemo(() => {
    const current = currentPath?.[0];
    if (!current) return false;
    if (current === item.href) return true;
    return (
      Array.isArray(item.activeHrefs) && item.activeHrefs.includes(current)
    );
  }, [currentPath, item.href, item.activeHrefs]);

  const hasSubMenu =
    Array.isArray(item.children) ||
    typeof item.customSubMenuRender === "function";
  const hasCustomSubMenuRender = typeof item.customSubMenuRender === "function";

  const onClickHandler = useCallback(() => {
    if (hasSubMenu && !hasCustomSubMenuRender) {
      return;
    }
    onClick?.([item]);
  }, [hasSubMenu, hasCustomSubMenuRender, item, onClick]);

  const buttonRender = () => {
    if (typeof customRender === "function") {
      return customRender({
        name: item.name,
        href: item.href,
        isActive,
      });
    }
    const button = (
      <button
        id={item.id}
        data-testid={item.testid}
        {...buttonProps}
        disabled={item.disabled}
        data-actived={isActive}
        className={cn(
          "oui-group oui-relative oui-h-[32px] oui-rounded oui-px-3 oui-py-1 oui-text-sm oui-text-base-contrast-36 hover:oui-bg-base-7",
          item.className,
          classNames?.navItem,
        )}
        onClick={onClickHandler}
      >
        <span className={"oui-flex oui-items-center"}>
          <ItemIcon isActive={isActive} item={item} />
          <Text.gradient
            color={isActive ? "brand" : "inherit"}
            angle={45}
            className="oui-whitespace-nowrap oui-break-normal"
          >
            {item.label ?? item.name}
          </Text.gradient>
          {hasSubMenu && (
            <span className={"oui-ms-1 group-data-[open=true]:oui-rotate-180"}>
              {isActive ? (
                <ActiveIcon />
              ) : (
                <ChevronDownIcon size={12} color={"white"} />
              )}
            </span>
          )}
        </span>
        <Box
          invisible={!isActive}
          position="absolute"
          bottom={0}
          left={"50%"}
          height={"3px"}
          r="full"
          width={"calc(100% - 24px)"} // oui-px-3 * 2
          gradient="brand"
          angle={45}
          className="-oui-translate-x-1/2 "
        />
      </button>
    );

    if (isObject(tooltipConfig) && tooltipConfig.showOnFirstVisit) {
      return (
        <Tooltip
          open={showButtonTooltip}
          content={tooltipConfig.text}
          className={
            "oui-w-64 oui-max-w-64 oui-bg-base-6 oui-text-2xs oui-font-semibold"
          }
        >
          {button}
        </Tooltip>
      );
    }
    return button;
  };

  if (onlyInMainAccount && !isMainAccount) {
    return null;
  }

  if (!hasSubMenu) {
    return buttonRender();
  }

  return (
    <SubMenus
      items={item.children || []}
      className={classNames?.subMenu}
      current={currentPath?.[1]}
      onItemClick={(subItem: MainNavItem) => {
        onClick?.([item, subItem]);
      }}
      customSubMenuRender={item.customSubMenuRender}
    >
      {buttonRender()}
    </SubMenus>
  );
};

const SubMenus: React.FC<
  PropsWithChildren<{
    items: MainNavItem[];
    className?: string;
    current?: string;
    onItemClick: (item: MainNavItem) => void;
    customSubMenuRender?: () => React.ReactNode;
  }>
> = (props) => {
  const {
    children,
    items,
    className,
    current,
    onItemClick,
    customSubMenuRender,
  } = props;
  const [open, setOpen] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const onMouseEnter = useCallback(() => {
    if (timer.current) {
      clearTimeout(timer.current);
      timer.current = null;
    }
    // setOpen(true);
  }, []);

  useEffect(() => {
    return () => {
      if (timer.current) {
        clearTimeout(timer.current);
      }
    };
  }, []);

  const classNames = (children as ReactElement).props?.className;

  return (
    <PopoverRoot open={open}>
      <PopoverAnchor>
        <div
          className={"oui-group"}
          data-open={open}
          onMouseEnter={() => {
            setOpen(true);
          }}
          onMouseLeave={() => {
            timer.current = setTimeout(() => {
              setOpen(false);
            }, 150);
          }}
        >
          {cloneElement(children as React.ReactElement<any>, {
            className: cn(classNames, open && "oui-bg-base-7"),
          })}
        </div>
      </PopoverAnchor>
      <PopoverContent
        align="start"
        onMouseEnter={onMouseEnter}
        onMouseLeave={() => {
          setOpen(false);
          if (timer.current) {
            clearTimeout(timer.current);
          }
        }}
        className={cn(
          "oui-subMenu",
          customSubMenuRender
            ? "oui-w-auto oui-rounded-lg oui-border-0 oui-p-0"
            : "oui-w-[200px] oui-space-y-[2px] oui-border oui-border-line-6 oui-p-1",
          className,
        )}
      >
        {customSubMenuRender
          ? customSubMenuRender()
          : items.map((item, index) => {
              return (
                <SubMenu
                  key={index}
                  item={item}
                  onClick={onItemClick}
                  active={item.href === current}
                />
              );
            })}
      </PopoverContent>
    </PopoverRoot>
  );
};

const SubMenu: React.FC<{
  item: MainNavItem;
  active?: boolean;
  onClick: (item: MainNavItem) => void;
}> = (props) => {
  const { item, active, onClick } = props;
  const hasDescription = typeof item.description !== "undefined";
  const hasIcon = typeof item.icon !== "undefined" && item.icon !== null;
  return (
    <Flex
      px={2}
      py={3}
      gapX={2}
      data-active={active ?? false}
      itemAlign={hasDescription ? "start" : "center"}
      className={cn(
        "oui-group oui-w-full oui-cursor-pointer oui-text-base-contrast-80 hover:oui-bg-base-6 data-[active=true]:oui-bg-base-5",
        // props.active && "oui-bg-base-5"
      )}
      r={"md"}
      onClick={() => {
        onClick(item);
      }}
      data-testid={item.testid}
    >
      {hasIcon && (
        <div
          className={cn(
            "oui-relative oui-size-6",
            hasDescription && "oui-translate-y-1",
          )}
        >
          <ItemIcon isActive={active ?? false} item={item} />
        </div>
      )}
      <div className="oui-flex-1">
        <SubMenuTitle item={item} isActive={active} />
        {hasDescription && (
          <Text size={"2xs"} as={"div"} intensity={36}>
            {item.description}
          </Text>
        )}
      </div>
    </Flex>
  );
};

const SubMenuTitle: React.FC<{ item: MainNavItem; isActive?: boolean }> = (
  props,
) => {
  const { item, isActive } = props;
  return (
    <Flex itemAlign={"center"} width={"100%"} position="relative">
      <div className="oui-flex oui-flex-1">
        <Text.gradient
          color={isActive ? "brand" : "inherit"}
          size={"xs"}
          as={"div"}
          intensity={80}
          weight={"semibold"}
          className="oui-whitespace-nowrap oui-break-normal"
        >
          {item.name}
        </Text.gradient>
        {typeof item.tag !== "undefined" && <Tag item={item} />}
      </div>
      {/* {item.target === "_blank" && <OutlinkIcon />} */}
    </Flex>
  );
};

const Tag: React.FC<{ item: MainNavItem }> = (props) => {
  return (
    <div
      className={
        "oui-ms-1 oui-inline-flex oui-rounded oui-bg-gradient-to-r oui-from-[rgb(var(--oui-gradient-brand-start)_/_0.12)] oui-to-[rgb(var(--oui-gradient-brand-end)_/_0.12)] oui-px-2 oui-py-1"
      }
    >
      <Text.gradient
        color={"brand"}
        size={"3xs"}
        className="oui-whitespace-nowrap oui-break-normal"
      >
        {props.item.tag}
      </Text.gradient>
    </div>
  );
};

const ICON_CLASSNAME =
  "oui-flex oui-border oui-border-line oui-w-6 oui-h-6 oui-rounded-md oui-justify-center oui-items-center oui-absolute oui-start-0 oui-top-0";

const ItemIcon: React.FC<{ item: MainNavItem; isActive: boolean }> = (
  props,
) => {
  const { isActive } = props;
  if (!props.item.icon) {
    return null;
  }
  if (typeof props.item.icon === "string") {
    return (
      <span className={"oui-me-1 oui-size-[20px]"}>
        {
          <img
            src={
              isActive
                ? (props.item.activeIcon as string) || props.item.icon
                : props.item.icon
            }
            className={"oui-max-h-full oui-max-w-full"}
          />
        }
      </span>
    );
  }
  // return isActive ? props.item.activeIcon || props.item.icon : props.item.icon;
  return (
    <>
      <div
        className={cn(
          ICON_CLASSNAME,
          "group-hover:oui-invisible group-data-[active=true]:oui-invisible",
        )}
      >
        {props.item.icon}
      </div>
      <div
        className={cn(
          ICON_CLASSNAME,
          "oui-invisible group-hover:oui-visible group-data-[active=true]:oui-visible",
        )}
      >
        {props.item.activeIcon || props.item.icon}
      </div>
    </>
  );
};

const OutlinkIcon: React.FC = () => {
  return (
    <>
      <svg
        width="20"
        height="20"
        viewBox="0 0 20 20"
        fill="none"
        xmlns="http://www.w3.org/2000/svg"
        className="oui-absolute oui-end-0 oui-top-0 group-hover:oui-invisible"
      >
        <path
          className="oui-fill-base-contrast-20"
          d="M14.159 17.492a3.333 3.333 0 0 0 3.333-3.333V5.826a3.333 3.333 0 0 0-3.333-3.334H5.826a3.333 3.333 0 0 0-3.334 3.334v8.333a3.333 3.333 0 0 0 3.334 3.333zm-6.667-4.166a.85.85 0 0 1-.599-.235.86.86 0 0 1 0-1.198l3.333-3.333-1.9-1.901h5v5l-1.901-1.9L8.09 13.09a.84.84 0 0 1-.599.235"
          fill="currentColor"
        />
      </svg>

      <svg
        width="20"
        height="20"
        viewBox="0 0 20 20"
        fill="none"
        xmlns="http://www.w3.org/2000/svg"
        className="oui-invisible oui-absolute oui-end-0 oui-top-0 group-hover:oui-visible"
      >
        <path
          d="M14.159 17.492a3.333 3.333 0 0 0 3.333-3.333V5.826a3.333 3.333 0 0 0-3.333-3.334H5.826a3.333 3.333 0 0 0-3.334 3.334v8.333a3.333 3.333 0 0 0 3.334 3.333zm-6.667-4.166a.85.85 0 0 1-.599-.235.86.86 0 0 1 0-1.198l3.333-3.333-1.9-1.901h5v5l-1.901-1.9L8.09 13.09a.84.84 0 0 1-.599.235"
          fill="url(#outlineIcon)"
        />
        <defs>
          <linearGradient
            id="outlineIcon"
            x1="17.492"
            y1="9.992"
            x2="2.492"
            y2="9.992"
            gradientUnits="userSpaceOnUse"
          >
            <stop stopColor="rgb(var(--oui-gradient-brand-end))" />
            <stop offset="1" stopColor="rgb(var(--oui-gradient-brand-start))" />
          </linearGradient>
        </defs>
      </svg>
    </>
  );
};
