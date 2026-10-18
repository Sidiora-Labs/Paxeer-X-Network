import type React from "react";
import type { SVGProps } from "react";

export interface MarketIconProps extends SVGProps<SVGSVGElement> {
  isSelected?: boolean;
  isHovered?: boolean;
}

/**
 * Markets-left layout preview icon.
 * Copied from trading SwitchLayout to keep visual style consistent.
 */
export const MarketLeftIcon: React.FC<MarketIconProps> = ({
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
