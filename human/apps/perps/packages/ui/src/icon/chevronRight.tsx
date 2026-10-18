import React from "react";
import { BaseIcon, BaseIconProps } from "./baseIcon";

export const ChevronRightIcon = React.forwardRef<SVGSVGElement, BaseIconProps>(
  (props, ref) => {
    const { opacity = 0.54, ...rest } = props;
    return (
      <BaseIcon ref={ref} {...rest}>
        <path
          fill="currentcolor"
          fillOpacity={opacity}
          d="M10.166 5.022c-.25-.051-.519-.03-.748.125a1.027 1.027 0 0 0-.28 1.406l3.612 5.437-3.612 5.438c-.305.459-.177 1.1.28 1.406a1.019 1.019 0 0 0 1.401-.281l3.986-6a1.03 1.03 0 0 0 0-1.125l-3.986-6c-.152-.23-.403-.356-.653-.406Z"
        />
      </BaseIcon>
    );
  }
);

ChevronRightIcon.displayName = "ChevronRightIcon";
