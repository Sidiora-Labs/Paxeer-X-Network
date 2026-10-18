import React from "react";
import { BaseIcon, BaseIconProps } from "./baseIcon";

export const CaretUpIcon = React.forwardRef<SVGSVGElement, BaseIconProps>(
  (props, ref) => {
    const { opacity = 0.54, ...rest } = props;
    return (
      <BaseIcon ref={ref} {...rest}>
        <path
          fill="currentcolor"
          fillOpacity={opacity}
          d="M12.017 5.997c-.3 0-.612.14-.812.406l-6 8c-.495.66-.012 1.594.812 1.594h12c.824 0 1.307-.935.812-1.594l-6-8a1.022 1.022 0 0 0-.812-.406Z"
        />
      </BaseIcon>
    );
  }
);

CaretUpIcon.displayName = "CaretUpIcon";
