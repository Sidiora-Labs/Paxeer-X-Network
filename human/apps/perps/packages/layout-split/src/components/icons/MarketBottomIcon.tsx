import type React from "react";
import type { MarketIconProps } from "./MarketLeftIcon";

/**
 * Markets-bottom layout preview icon.
 * Copied from trading SwitchLayout to keep visual style consistent.
 */
export const MarketBottomIcon: React.FC<MarketIconProps> = ({
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
