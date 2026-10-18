import React from "react";
import {
  Box,
  Flex,
  tv,
  VariantProps,
  Text,
  cn,
  Tooltip,
} from "@paxeer-network/ui";

type SideMenuItem = {
  name: string;
  icon?: React.ReactNode;
  href?: string;
  disabled?: boolean;
  onClick?: () => void;
  hide?: boolean;
};

const menuItemVariants = tv({
  slots: {
    button: [
      "oui-min-h-10",
      "oui-px-3",
      "oui-py-2",
      "oui-rounded-md",
      "oui-w-full",
      "oui-text-start",
      "oui-text-base",
      "oui-text-base-contrast-36",
      // "oui-flex",
      "oui-group",
      // "oui-space-x-2",
      // "oui-items-center",
      "hover:oui-bg-base-8",
      "oui-transition-colors",
      "group-data-[state=closed]/bar:oui-w-[42px]",
      "oui-overflow-hidden",
    ],
    icon: [],
  },
  variants: {
    mode: {
      "icon-only": {
        button: "oui-w-10",
        icon: "w-6 h-6",
      },
      full: {
        button: "oui-full",
        icon: "w-6 h-6",
      },
    },
    active: {
      true: {
        button: "oui-bg-base-5 hover:oui-bg-base-5",
      },
    },
    open: {
      true: {
        button: "",
      },
    },
  },
});

const MenuItem = React.memo<
  {
    item: SideMenuItem;
    active?: boolean;
    open?: boolean;
    onClick?: (item: SideMenuItem) => void;
  } & VariantProps<typeof menuItemVariants>
>((props) => {
  const { item, mode, open, onClick, active, ...rest } = props;
  const { button } = menuItemVariants({
    mode,
    active: props.active,
    open: props.open,
  });
  const children = (
    <button
      data-actived={props.active}
      disabled={item.disabled}
      className={button()}
      onClick={() => {
        props.onClick?.(item);
      }}
    >
      <Flex itemAlign={"center"} gap={2} as="span">
        <div>{item.icon}</div>
        {props.open && (
          <Text.gradient
            color={props.active ? "brand" : "inherit"}
            angle={45}
            size="base"
            className="oui-break-all oui-animate-in oui-fade-in"
          >
            {item.name}
          </Text.gradient>
        )}
      </Flex>
    </button>
  );

  if (props.open) {
    return <li className="oui-min-w-[120px]">{children}</li>;
  }

  return (
    <li>
      <Tooltip content={item.name} side="right" align="center" sideOffset={20}>
        {children}
      </Tooltip>
    </li>
  );
});

MenuItem.displayName = "LeftMenuItem";

const SideMenus: React.FC<{
  menus?: SideMenuItem[];
  current?: string;
  open?: boolean;
  onItemSelect?: (item: SideMenuItem) => void;
}> = (props) => {
  return (
    <Box py={6}>
      <svg
        width="18"
        height="18"
        viewBox="0 0 18 18"
        fill="none"
        xmlns="http://www.w3.org/2000/svg"
        className="oui-pointer-events-none oui-invisible oui-absolute"
      >
        <defs>
          <linearGradient
            id="side-menu-gradient"
            x1="15.7432"
            y1="8.94726"
            x2="2.24316"
            y2="8.94726"
            gradientUnits="userSpaceOnUse"
          >
            <stop stopColor="rgb(var(--oui-gradient-brand-end))" />
            <stop stopColor="rgb(var(--oui-gradient-brand-start))" offset="1" />
          </linearGradient>
        </defs>
      </svg>
      <ul className="oui-space-y-4">
        {props.menus?.map((item, index) => {
          if (item?.hide) {
            return null;
          }
          return (
            <MenuItem
              key={index}
              item={item}
              open={props.open}
              active={item.href === props.current}
              onClick={props.onItemSelect}
            />
          );
        })}
      </ul>
    </Box>
  );
};

type SideBarHeaderProps = {
  onToggle?: () => void;
  open?: boolean;
  title?: React.ReactNode;
};

const SideBarHeader: React.FC<SideBarHeaderProps> = (props) => {
  const { title } = props;

  const titleElemet =
    typeof title === "string" ? (
      <Text size="xs" className="oui-text-primary">
        {title}
      </Text>
    ) : (
      title
    );

  const iconProps = {
    className:
      "oui-text-base-contrast-36 hover:oui-text-base-contrast-80 oui-cursor-pointer",
    onClick: props.onToggle,
  };

  return (
    <Flex
      justify={props.open ? "between" : "center"}
      itemAlign="center"
      className="oui-h-6"
    >
      {props.open ? titleElemet : null}

      {props.open ? (
        <CollapseIcon {...iconProps} />
      ) : (
        <ExpandIcon {...iconProps} />
      )}
    </Flex>
  );
};

type SideBarProps = {
  title?: React.ReactNode;
  items?: SideMenuItem[];
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
  onItemSelect?: (item: SideMenuItem) => void;
  current?: string;
  className?: string;
  maxWidth?: number;
  minWidth?: number;
  style?: React.CSSProperties;
};

const SideBar: React.FC<SideBarProps> = (props) => {
  const { open = true, items, current, onItemSelect } = props;

  return (
    <Box
      data-state={open ? "opened" : "closed"}
      className={cn("oui-group/bar", props.className)}
      style={props.style}
    >
      <SideBarHeader
        open={open}
        title={props.title}
        onToggle={() => {
          props.onOpenChange?.(!open);
        }}
      />
      <SideMenus
        menus={items}
        current={current}
        onItemSelect={onItemSelect}
        open={open}
      />
    </Box>
  );
};

SideBar.displayName = "SideBar";

const ExpandIcon: React.FC<React.SVGProps<SVGSVGElement>> = (props) => (
  <svg
    width="16"
    height="16"
    viewBox="0 0 16 16"
    fill="currentColor"
    xmlns="http://www.w3.org/2000/svg"
    {...props}
  >
    <path d="M6.326 8.826a.84.84 0 0 0-.6.234L2.16 12.627v-2.135H.492v4.167c0 .46.373.833.834.833h4.166v-1.667H3.357l3.567-3.567a.857.857 0 0 0 0-1.198.84.84 0 0 0-.598-.234M10.502.492V2.16h2.135L9.07 5.726a.857.857 0 0 0 0 1.199.86.86 0 0 0 1.197 0l3.568-3.568v2.135h1.667V1.326a.834.834 0 0 0-.834-.834z" />
  </svg>
);

const CollapseIcon: React.FC<React.SVGProps<SVGSVGElement>> = (props) => (
  <svg
    width="16"
    height="16"
    viewBox="0 0 16 16"
    fill="currentColor"
    xmlns="http://www.w3.org/2000/svg"
    {...props}
  >
    <path d="M14.668.492a.85.85 0 0 0-.599.234l-3.567 3.568V2.159H8.835v4.167c0 .46.373.833.833.833h4.167V5.492H11.7l3.569-3.567a.86.86 0 0 0 0-1.199.85.85 0 0 0-.6-.234m-12.5 8.334v1.666h2.135L.736 14.06a.86.86 0 0 0 0 1.198.86.86 0 0 0 1.198 0l3.568-3.567v2.134h1.666V9.66a.834.834 0 0 0-.833-.833z" />
  </svg>
);

export { SideBar };

export type { SideBarProps, SideMenuItem };
