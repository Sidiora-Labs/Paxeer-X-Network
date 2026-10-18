import { FC, PropsWithChildren, SVGProps, useState } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import {
  Box,
  CloseIcon,
  cn,
  Divider,
  DropdownMenuContent,
  DropdownMenuPortal,
  DropdownMenuRoot,
  DropdownMenuTrigger,
  Flex,
  Text,
} from "@paxeer-network/ui";
import type { MarketLayoutPosition } from "../../../pages/trading/trading.script";

export type LayoutPosition = "left" | "right";

export type SwitchLayoutProps = {
  layout?: LayoutPosition;
  onLayout?: (layout: LayoutPosition) => void;
  marketLayout?: MarketLayoutPosition;
  onMarketLayout?: (layout: MarketLayoutPosition) => void;
};

export const SwitchLayout: FC<SwitchLayoutProps> = (props) => {
  const { t } = useTranslation();
  return (
    <SwitchLayoutDropDown {...props}>
      <Flex
        px={3}
        className={cn(
          "oui-switchLayout",
          "oui-rounded-md",
          "oui-h-[28px]",
          "oui-cursor-pointer oui-transition-all",
          "oui-bg-base-6 hover:oui-bg-base-4",
          "oui-text-base-contrast-54 hover:oui-text-base-contrast-80",
        )}
        gapX={1}
        ml={3}
        justify="center"
        itemAlign="center"
      >
        <LayoutIcon />
        <Text size="2xs" weight="semibold">
          {t("trading.layout")}
        </Text>
      </Flex>
    </SwitchLayoutDropDown>
  );
};

export const SwitchLayoutDropDown: FC<PropsWithChildren<SwitchLayoutProps>> = (
  props,
) => {
  const [open, setOpen] = useState(false);
  const [hoveredMarket, setHoveredMarket] =
    useState<MarketLayoutPosition | null>(null);
  const { t } = useTranslation();
  const renderItem = (position: LayoutPosition) => {
    return (
      <Flex
        direction="column"
        gapY={2}
        onClick={() => {
          props.onLayout?.(position);
          setOpen(false);
        }}
        className="oui-group"
      >
        <Flex
          justify={position === "right" ? "end" : "start"}
          className={cn(
            "oui-w-[148px] oui-h-[100px]",
            "oui-bg-base-10 oui-rounded-[10px]",
            "oui-border-[4px] oui-border-base-5 group-hover:oui-border-primary-light",
            props.layout === position && "!oui-border-primary-light",
          )}
        >
          <Box p={1}>
            <OrderEntryIcon />
          </Box>
        </Flex>
        <Text
          size="2xs"
          intensity={54}
          className={cn(
            "oui-text-base-contrast-54 group-hover:oui-text-base-contrast-80",
            props.layout === position && "oui-text-base-contrast-80",
          )}
        >
          {String(
            position === "right"
              ? t("trading.layout.advanced.right")
              : t("trading.layout.advanced.left"),
          )}
        </Text>
      </Flex>
    );
  };

  const renderMarketItem = (position: MarketLayoutPosition) => {
    const getIcon = (isHovered: boolean) => {
      const isSelected = props.marketLayout === position;
      switch (position) {
        case "left":
          return (
            <MarketLeftIcon isSelected={isSelected} isHovered={isHovered} />
          );
        case "top":
          return (
            <MarketTopIcon isSelected={isSelected} isHovered={isHovered} />
          );
        case "bottom":
          return (
            <MarketBottomIcon isSelected={isSelected} isHovered={isHovered} />
          );
        case "hide":
          return (
            <MarketHideIcon isSelected={isSelected} isHovered={isHovered} />
          );
        default:
          return (
            <MarketLeftIcon isSelected={isSelected} isHovered={isHovered} />
          );
      }
    };

    const getLabel = () => {
      switch (position) {
        case "left":
          return t("trading.layout.markets.left");
        case "top":
          return t("trading.layout.markets.top");
        case "bottom":
          return t("trading.layout.markets.bottom");
        case "hide":
          return t("trading.layout.markets.hide");
        default:
          return t("trading.layout.markets.left");
      }
    };

    return (
      <Flex
        direction="column"
        gapY={2}
        onClick={() => {
          props.onMarketLayout?.(position);
          setOpen(false);
        }}
        onMouseEnter={() => setHoveredMarket(position)}
        onMouseLeave={() => setHoveredMarket(null)}
        className="oui-group"
      >
        <Flex justify="center" className="oui-w-[148px] oui-h-[100px]">
          {getIcon(hoveredMarket === position)}
        </Flex>
        <Text
          size="2xs"
          intensity={54}
          className={cn(
            "oui-text-base-contrast-54 group-hover:oui-text-base-contrast-80",
            props.marketLayout === position && "oui-text-base-contrast-80",
          )}
        >
          {String(getLabel())}
        </Text>
      </Flex>
    );
  };

  const content = (
    <>
      <Flex
        itemAlign="center"
        justify="between"
        mt={3}
        className="oui-mb-[10px] oui-min-w-[500px]"
      >
        <Text size="base" intensity={98}>
          {t("trading.layout")}
        </Text>
        <CloseIcon
          size={16}
          className="oui-text-base-contrast-80 oui-cursor-pointer"
          opacity={0.98}
          onClick={() => {
            setOpen(false);
          }}
        />
      </Flex>
      <Divider />
      <Flex direction="column" gapY={2} mt={5} itemAlign="start">
        <Text size="xs" intensity={98}>
          {String(t("trading.layout.advanced"))}
        </Text>
        <Flex gapX={6}>
          {renderItem("left")}
          {renderItem("right")}
        </Flex>
      </Flex>
      <Flex direction="column" gapY={2} mt={5} itemAlign="start">
        <Text size="xs" intensity={98}>
          {String(t("trading.layout.markets"))}
        </Text>
        <Flex gapX={6}>
          {renderMarketItem("left")}
          {renderMarketItem("top")}
          {renderMarketItem("bottom")}
          {renderMarketItem("hide")}
        </Flex>
      </Flex>
    </>
  );

  return (
    <DropdownMenuRoot open={open} onOpenChange={setOpen}>
      <DropdownMenuTrigger asChild>{props.children}</DropdownMenuTrigger>
      <DropdownMenuPortal>
        <DropdownMenuContent
          onCloseAutoFocus={(e) => e.preventDefault()}
          onClick={(e) => e.stopPropagation()}
          align="end"
          className={cn("oui-bg-base-8 oui-p-5 oui-pt-0 oui-font-semibold")}
        >
          {content}
        </DropdownMenuContent>
      </DropdownMenuPortal>
    </DropdownMenuRoot>
  );
};

export const LayoutIcon: FC<SVGProps<SVGSVGElement>> = (props) => (
  <svg
    width="17"
    height="16"
    viewBox="0 0 17 16"
    fill="currentColor"
    xmlns="http://www.w3.org/2000/svg"
    {...props}
  >
    <path d="M3.832 1.994c-.736 0-1.333.597-1.333 1.333v9.334c0 .737.597 1.333 1.333 1.333H6.5c.737 0 1.333-.596 1.333-1.333V3.327c0-.736-.596-1.333-1.333-1.333zm6.667 0c-.737 0-1.333.597-1.333 1.333v2.667c0 .737.596 1.333 1.333 1.333h2.667c.736 0 1.333-.596 1.333-1.333V3.327c0-.736-.597-1.333-1.333-1.333zm.437 6.679a2.7 2.7 0 0 0-1.033.607.284.284 0 0 0-.061.339c.222.411-.01.851-.512.876a.29.29 0 0 0-.26.217c-.05.207-.07.38-.07.608 0 .19.02.407.06.599a.28.28 0 0 0 .252.217c.506.044.756.429.53.92a.28.28 0 0 0 .06.321c.296.273.635.466 1.034.59a.285.285 0 0 0 .312-.104c.31-.427.757-.428 1.05 0a.28.28 0 0 0 .313.113 2.8 2.8 0 0 0 1.042-.599.28.28 0 0 0 .06-.33c-.23-.466.035-.894.513-.902a.28.28 0 0 0 .269-.209c.048-.199.06-.372.06-.616q0-.316-.069-.616a.276.276 0 0 0-.27-.217c-.469-.001-.732-.463-.502-.868a.27.27 0 0 0-.053-.339 2.8 2.8 0 0 0-1.059-.607.274.274 0 0 0-.312.112c-.268.417-.77.425-1.033.009a.284.284 0 0 0-.321-.121m.842 1.536a1.111 1.111 0 1 1 0 2.222 1.111 1.111 0 0 1 0-2.222" />
  </svg>
);

export const OrderEntryIcon: FC<SVGProps<SVGSVGElement>> = (props) => (
  <svg
    width="36"
    height="84"
    viewBox="0 0 36 84"
    fill="none"
    xmlns="http://www.w3.org/2000/svg"
  >
    <rect width="36" height="17" rx="2" fill="rgb(var(--oui-color-base-7))" />
    <rect
      y="19"
      width="36"
      height="54"
      rx="2"
      fill="rgb(var(--oui-color-base-7))"
    />
    <rect
      y="75"
      width="36"
      height="9"
      rx="2"
      fill="rgb(var(--oui-color-base-7))"
    />
    <rect
      x="3"
      y="79"
      width="30"
      height="1"
      rx="0.5"
      fill="url(#paint0_linear_17647_26849)"
    />
    <rect
      x="3"
      y="22"
      width="14"
      height="6"
      rx="2"
      fill="rgb(var(--oui-color-success-darken))"
    />
    <rect
      x="19"
      y="22"
      width="14"
      height="6"
      rx="2"
      fill="rgb(var(--oui-color-danger-darken))"
    />
    <rect
      x="3"
      y="11"
      width="14"
      height="3"
      rx="1.5"
      fill="rgb(var(--oui-color-base-4))"
    />
    <rect
      x="19"
      y="11"
      width="14"
      height="3"
      rx="1.5"
      fill="rgb(var(--oui-color-primary-darken))"
    />
    <rect
      x="3"
      y="62"
      width="30"
      height="8"
      rx="2"
      fill="rgb(var(--oui-color-success-darken))"
    />
    <defs>
      <linearGradient
        id="paint0_linear_17647_26849"
        x1="33"
        y1="79.5"
        x2="3"
        y2="79.5"
        gradientUnits="userSpaceOnUse"
      >
        <stop stopColor="rgb(var(--oui-color-primary-light))" />
        <stop offset="1" stopColor="rgb(var(--oui-gradient-brand-start))" />
      </linearGradient>
    </defs>
  </svg>
);

export const MarketLeftIcon: FC<
  SVGProps<SVGSVGElement> & { isSelected?: boolean; isHovered?: boolean }
> = ({ isSelected, isHovered, ...props }) => {
  const getStrokeColor = () => {
    if (isSelected) return "rgb(var(--oui-color-primary-light))"; // primary-light color
    if (isHovered) return "rgb(var(--oui-color-primary-light))"; // primary-light color
    return "rgb(var(--oui-color-base-5))"; // base-5 color
  };

  return (
    <svg
      width="148"
      height="100"
      viewBox="0 0 148 100"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      {...props}
    >
      <rect
        x="2"
        y="2"
        width="144"
        height="96"
        rx="10"
        fill="rgb(var(--oui-color-base-10))"
        stroke={getStrokeColor()}
        strokeWidth="4"
      />
      <rect
        x="8"
        y="8"
        width="24"
        height="84"
        rx="2"
        fill="rgb(var(--oui-color-base-7))"
      />
      <rect
        x="16"
        y="10"
        width="4"
        height="2"
        rx="1"
        fill="rgb(var(--oui-color-base-4))"
      />
      <rect
        x="21"
        y="10"
        width="4"
        height="2"
        rx="1"
        fill="rgb(var(--oui-color-base-4))"
      />
      <rect
        x="26"
        y="10"
        width="4"
        height="2"
        rx="1"
        fill="rgb(var(--oui-color-base-4))"
      />
      <rect
        x="10"
        y="10"
        width="5"
        height="2"
        rx="1"
        fill="rgb(var(--oui-color-primary-darken))"
      />
      <rect
        x="10"
        y="14"
        width="20"
        height="76"
        rx="2"
        fill="rgb(var(--oui-color-base-5))"
      />
    </svg>
  );
};

export const MarketTopIcon: FC<
  SVGProps<SVGSVGElement> & { isSelected?: boolean; isHovered?: boolean }
> = ({ isSelected, isHovered, ...props }) => {
  const getStrokeColor = () => {
    if (isSelected) return "rgb(var(--oui-color-primary-light))"; // primary-light color
    if (isHovered) return "rgb(var(--oui-color-primary-light))"; // primary-light color
    return "rgb(var(--oui-color-base-5))"; // base-5 color
  };

  return (
    <svg
      width="148"
      height="100"
      viewBox="0 0 148 100"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      {...props}
    >
      <rect
        x="2"
        y="2"
        width="144"
        height="96"
        rx="10"
        fill="rgb(var(--oui-color-base-10))"
        stroke={getStrokeColor()}
        strokeWidth="4"
      />
      <rect
        x="8"
        y="8"
        width="132"
        height="8"
        rx="2"
        fill="rgb(var(--oui-color-base-7))"
      />
      <g clipPath="url(#clip0_31319_74729)">
        <rect
          x="10"
          y="10"
          width="16"
          height="4"
          rx="2"
          fill="rgb(var(--oui-color-primary-darken))"
        />
        <rect
          x="28"
          y="10"
          width="16"
          height="4"
          rx="2"
          fill="rgb(var(--oui-color-base-4))"
        />
        <rect
          x="46"
          y="10"
          width="16"
          height="4"
          rx="2"
          fill="rgb(var(--oui-color-base-4))"
        />
        <rect
          x="64"
          y="10"
          width="16"
          height="4"
          rx="2"
          fill="rgb(var(--oui-color-base-4))"
        />
        <rect
          x="82"
          y="10"
          width="16"
          height="4"
          rx="2"
          fill="rgb(var(--oui-color-base-4))"
        />
        <rect
          x="100"
          y="10"
          width="16"
          height="4"
          rx="2"
          fill="rgb(var(--oui-color-base-4))"
        />
        <rect
          x="118"
          y="10"
          width="16"
          height="4"
          rx="2"
          fill="rgb(var(--oui-color-base-4))"
        />
        <rect
          x="136"
          y="10"
          width="16"
          height="4"
          rx="2"
          fill="rgb(var(--oui-color-base-4))"
        />
      </g>
      <defs>
        <clipPath id="clip0_31319_74729">
          <rect
            width="130"
            height="4"
            fill="rgb(var(--oui-color-base-foreground))"
            transform="translate(10 10)"
          />
        </clipPath>
      </defs>
    </svg>
  );
};

export const MarketBottomIcon: FC<
  SVGProps<SVGSVGElement> & { isSelected?: boolean; isHovered?: boolean }
> = ({ isSelected, isHovered, ...props }) => {
  const getStrokeColor = () => {
    if (isSelected) return "rgb(var(--oui-color-primary-light))"; // primary-light color
    if (isHovered) return "rgb(var(--oui-color-primary-light))"; // primary-light color
    return "rgb(var(--oui-color-base-5))"; // base-5 color
  };

  return (
    <svg
      width="148"
      height="100"
      viewBox="0 0 148 100"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      {...props}
    >
      <rect
        x="2"
        y="2"
        width="144"
        height="96"
        rx="10"
        fill="rgb(var(--oui-color-base-10))"
        stroke={getStrokeColor()}
        strokeWidth="4"
      />
      <rect
        x="8"
        y="84"
        width="132"
        height="8"
        rx="2"
        fill="rgb(var(--oui-color-base-7))"
      />
      <g clipPath="url(#clip0_31319_74743)">
        <rect
          x="10"
          y="86"
          width="16"
          height="4"
          rx="2"
          fill="rgb(var(--oui-color-primary-darken))"
        />
        <rect
          x="28"
          y="86"
          width="16"
          height="4"
          rx="2"
          fill="rgb(var(--oui-color-base-4))"
        />
        <rect
          x="46"
          y="86"
          width="16"
          height="4"
          rx="2"
          fill="rgb(var(--oui-color-base-4))"
        />
        <rect
          x="64"
          y="86"
          width="16"
          height="4"
          rx="2"
          fill="rgb(var(--oui-color-base-4))"
        />
        <rect
          x="82"
          y="86"
          width="16"
          height="4"
          rx="2"
          fill="rgb(var(--oui-color-base-4))"
        />
        <rect
          x="100"
          y="86"
          width="16"
          height="4"
          rx="2"
          fill="rgb(var(--oui-color-base-4))"
        />
        <rect
          x="118"
          y="86"
          width="16"
          height="4"
          rx="2"
          fill="rgb(var(--oui-color-base-4))"
        />
        <rect
          x="136"
          y="86"
          width="16"
          height="4"
          rx="2"
          fill="rgb(var(--oui-color-base-4))"
        />
      </g>
      <defs>
        <clipPath id="clip0_31319_74743">
          <rect
            width="130"
            height="4"
            fill="rgb(var(--oui-color-base-foreground))"
            transform="translate(10 86)"
          />
        </clipPath>
      </defs>
    </svg>
  );
};

export const MarketHideIcon: FC<
  SVGProps<SVGSVGElement> & { isSelected?: boolean; isHovered?: boolean }
> = ({ isSelected, isHovered, ...props }) => {
  const getStrokeColor = () => {
    if (isSelected) return "rgb(var(--oui-color-primary-light))"; // primary-light color
    if (isHovered) return "rgb(var(--oui-color-primary-light))"; // primary-light color
    return "rgb(var(--oui-color-base-5))"; // base-5 color
  };

  return (
    <svg
      width="148"
      height="100"
      viewBox="0 0 148 100"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      {...props}
    >
      <rect
        x="2"
        y="2"
        width="144"
        height="96"
        rx="10"
        fill="rgb(var(--oui-color-base-10))"
        stroke={getStrokeColor()}
        strokeWidth="4"
      />
      <g clipPath="url(#clip0_31319_74757)">
        <rect
          x="8"
          y="8"
          width="132"
          height="84"
          rx="2"
          fill="rgb(var(--oui-color-base-7))"
        />
        <rect
          x="66.8789"
          y="-76"
          width="4"
          height="188"
          rx="2"
          transform="rotate(45 66.8789 -76)"
          fill="rgb(var(--oui-color-base-6))"
        />
        <rect
          x="73.9492"
          y="-68.929"
          width="4"
          height="188"
          rx="2"
          transform="rotate(45 73.9492 -68.929)"
          fill="rgb(var(--oui-color-base-6))"
        />
        <rect
          x="81.0195"
          y="-61.8579"
          width="4"
          height="188"
          rx="2"
          transform="rotate(45 81.0195 -61.8579)"
          fill="rgb(var(--oui-color-base-6))"
        />
        <rect
          x="88.0938"
          y="-54.7867"
          width="4"
          height="188"
          rx="2"
          transform="rotate(45 88.0938 -54.7867)"
          fill="rgb(var(--oui-color-base-6))"
        />
        <rect
          x="95.1641"
          y="-47.7157"
          width="4"
          height="188"
          rx="2"
          transform="rotate(45 95.1641 -47.7157)"
          fill="rgb(var(--oui-color-base-6))"
        />
        <rect
          x="102.234"
          y="-40.6447"
          width="4"
          height="188"
          rx="2"
          transform="rotate(45 102.234 -40.6447)"
          fill="rgb(var(--oui-color-base-6))"
        />
        <rect
          x="109.305"
          y="-33.5736"
          width="4"
          height="188"
          rx="2"
          transform="rotate(45 109.305 -33.5736)"
          fill="rgb(var(--oui-color-base-6))"
        />
        <rect
          x="116.375"
          y="-26.5026"
          width="4"
          height="188"
          rx="2"
          transform="rotate(45 116.375 -26.5026)"
          fill="rgb(var(--oui-color-base-6))"
        />
        <rect
          x="123.449"
          y="-19.4315"
          width="4"
          height="188"
          rx="2"
          transform="rotate(45 123.449 -19.4315)"
          fill="rgb(var(--oui-color-base-6))"
        />
        <rect
          x="130.52"
          y="-12.3604"
          width="4"
          height="188"
          rx="2"
          transform="rotate(45 130.52 -12.3604)"
          fill="rgb(var(--oui-color-base-6))"
        />
        <rect
          x="137.59"
          y="-5.28931"
          width="4"
          height="188"
          rx="2"
          transform="rotate(45 137.59 -5.28931)"
          fill="rgb(var(--oui-color-base-6))"
        />
        <rect
          x="144.66"
          y="1.78174"
          width="4"
          height="188"
          rx="2"
          transform="rotate(45 144.66 1.78174)"
          fill="rgb(var(--oui-color-base-6))"
        />
        <rect
          x="151.73"
          y="8.85278"
          width="4"
          height="188"
          rx="2"
          transform="rotate(45 151.73 8.85278)"
          fill="rgb(var(--oui-color-base-6))"
        />
        <rect
          x="158.805"
          y="15.9238"
          width="4"
          height="188"
          rx="2"
          transform="rotate(45 158.805 15.9238)"
          fill="rgb(var(--oui-color-base-6))"
        />
        <rect
          x="165.875"
          y="22.995"
          width="4"
          height="188"
          rx="2"
          transform="rotate(45 165.875 22.995)"
          fill="rgb(var(--oui-color-base-6))"
        />
        <rect
          x="172.945"
          y="30.066"
          width="4"
          height="188"
          rx="2"
          transform="rotate(45 172.945 30.066)"
          fill="rgb(var(--oui-color-base-6))"
        />
        <rect
          x="180.016"
          y="37.1371"
          width="4"
          height="188"
          rx="2"
          transform="rotate(45 180.016 37.1371)"
          fill="rgb(var(--oui-color-base-6))"
        />
        <rect
          x="187.086"
          y="44.2081"
          width="4"
          height="188"
          rx="2"
          transform="rotate(45 187.086 44.2081)"
          fill="rgb(var(--oui-color-base-6))"
        />
        <rect
          x="194.156"
          y="51.2792"
          width="4"
          height="188"
          rx="2"
          transform="rotate(45 194.156 51.2792)"
          fill="rgb(var(--oui-color-base-6))"
        />
        <rect
          x="201.23"
          y="58.3503"
          width="4"
          height="188"
          rx="2"
          transform="rotate(45 201.23 58.3503)"
          fill="rgb(var(--oui-color-base-6))"
        />
      </g>
      <defs>
        <clipPath id="clip0_31319_74757">
          <rect
            width="132"
            height="84"
            fill="rgb(var(--oui-color-base-foreground))"
            transform="translate(8 8)"
          />
        </clipPath>
      </defs>
    </svg>
  );
};
