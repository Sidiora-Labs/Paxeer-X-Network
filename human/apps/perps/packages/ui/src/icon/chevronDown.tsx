import React from "react";
import { BaseIcon, BaseIconProps } from "./baseIcon";

export const ChevronDownIcon = React.forwardRef<SVGSVGElement, BaseIconProps>(
  (props, ref) => {
    const { opacity = 0.54, ...rest } = props;
    return (
      <BaseIcon ref={ref} {...rest}>
        <path
          fill="currentcolor"
          fillOpacity={opacity}
          d="M5.827 9.03c-.25.05-.502.175-.655.404a1.019 1.019 0 0 0 .28 1.401l5.992 3.985c.334.223.787.223 1.122 0l5.992-3.985c.459-.305.587-.943.28-1.4a1.023 1.023 0 0 0-1.404-.28l-5.43 3.61-5.428-3.61c-.23-.154-.498-.176-.75-.126Z"
        />
      </BaseIcon>
    );
  }
);

ChevronDownIcon.displayName = "ChevronDownIcon";
