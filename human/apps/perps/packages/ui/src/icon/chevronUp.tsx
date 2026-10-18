import React from "react";
import { BaseIcon, BaseIconProps } from "./baseIcon";

export const ChevronUpIcon = React.forwardRef<SVGSVGElement, BaseIconProps>(
  (props, ref) => {
    const { opacity = 0.54, ...rest } = props;
    return (
      <BaseIcon ref={ref} {...rest}>
        <path
          fill="currentcolor"
          fillOpacity={opacity}
          d="m11.443 9.17-5.991 3.986a1.02 1.02 0 0 0-.28 1.4c.305.458.945.586 1.404.281l5.429-3.612 5.428 3.612a1.025 1.025 0 0 0 1.404-.28 1.02 1.02 0 0 0-.28-1.401l-5.99-3.986a1.028 1.028 0 0 0-1.124 0Z"
        />
      </BaseIcon>
    );
  }
);

ChevronUpIcon.displayName = "ChevronUpIcon";
