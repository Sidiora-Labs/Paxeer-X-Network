import React from "react";
import { BaseIcon, BaseIconProps } from "./baseIcon";

export const CaretLeftIcon = React.forwardRef<SVGSVGElement, BaseIconProps>(
  (props, ref) => {
    const { opacity = 0.54, ...rest } = props;
    return (
      <BaseIcon ref={ref} {...rest}>
        <path
          fill="currentcolor"
          fillOpacity={opacity}
          d="M15.475 5.113a1.014 1.014 0 0 0-1.063.094l-8 6a1.025 1.025 0 0 0 0 1.624l8 6c.659.495 1.594.012 1.594-.812v-12c0-.412-.214-.747-.531-.906Z"
        />
      </BaseIcon>
    );
  }
);

CaretLeftIcon.displayName = "CaretLeftIcon";
