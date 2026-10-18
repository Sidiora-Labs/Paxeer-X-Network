import React from "react";
import type { BaseIconProps } from "./baseIcon";

export const WarningIcon = React.forwardRef<SVGSVGElement, BaseIconProps>(
  (props, ref) => {
    return (
      <svg
        width={20}
        height={20}
        viewBox="0 0 20 20"
        fill="currentColor"
        xmlns="http://www.w3.org/2000/svg"
        ref={ref}
        {...props}
      >
        <path d="M10.012 1.666a8.333 8.333 0 1 0 0 16.667 8.333 8.333 0 1 0 0-16.667m0 4.167c.46 0 .833.373.833.833v4.167a.834.834 0 0 1-1.667 0V6.666c0-.46.374-.833.834-.833m0 6.666a.834.834 0 1 1 0 1.668.834.834 0 0 1 0-1.668" />
      </svg>
    );
  },
);
