import React from "react";
import { BaseIcon, BaseIconProps } from "./baseIcon";

export const RefreshIcon = React.forwardRef<SVGSVGElement, BaseIconProps>(
  (props, ref) => {
    const { opacity = 0.54, ...rest } = props;
    return (
      <BaseIcon ref={ref} {...rest}>
        <path
          fill="currentcolor"
          fillOpacity={opacity}
          d="M12.007 2.99a9 9 0 0 0-5.594 1.94 1 1 0 1 0 1.25 1.562 6.97 6.97 0 0 1 4.344-1.5 7 7 0 0 1 7 7h-2l3 4 3-4h-2a9 9 0 0 0-9-9m-8 5-3 4h2a9 9 0 0 0 9 9 9 9 0 0 0 5.594-1.937 1 1 0 1 0-1.25-1.562 6.96 6.96 0 0 1-4.344 1.5 7 7 0 0 1-7-7h2z"
        />
      </BaseIcon>
    );
  }
);

RefreshIcon.displayName = "RefreshIcon";
