import type React from "react";
import type { MarketIconProps } from "./MarketLeftIcon";

/**
 * Markets-hide layout preview icon.
 * Copied from trading SwitchLayout to keep visual style consistent.
 */
export const MarketHideIcon: React.FC<MarketIconProps> = ({
  isSelected,
  isHovered,
  ...props
}) => {
  const getStrokeColor = () => {
    if (isSelected) return "rgb(var(--oui-color-primary-light))";
    if (isHovered) return "rgb(var(--oui-color-primary-light))";
    return "rgb(var(--oui-color-base-5))";
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
