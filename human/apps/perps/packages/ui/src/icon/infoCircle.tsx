import React from "react";
import type { BaseIconProps } from "./baseIcon";

export const InfoCircleIcon = React.forwardRef<SVGSVGElement, BaseIconProps>(
  (props, ref) => {
    const { className, opacity = 0.36, ...rest } = props;

    return (
      <svg
        width="16"
        height="16"
        viewBox="0 0 16 16"
        fill="currentColor"
        xmlns="http://www.w3.org/2000/svg"
        opacity={opacity}
        className={className}
        ref={ref}
        {...rest}
      >
        <path
          d="M7.999 1.343a6.667 6.667 0 1 0 0 13.333 6.667 6.667 0 0 0 0-13.333m0 3.333a.667.667 0 1 1 0 1.334.667.667 0 0 1 0-1.334m0 2c.368 0 .666.299.666.667v3.333a.667.667 0 0 1-1.333 0V7.343c0-.368.298-.667.667-.667"
          fill="currentColor"
        />
      </svg>
    );
  },
);
