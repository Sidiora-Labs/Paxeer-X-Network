import React from "react";
import { BaseIcon, BaseIconProps } from "./baseIcon";

export const CloseIcon = React.forwardRef<SVGSVGElement, BaseIconProps>(
  (props, ref) => {
    const { opacity = 0.54, ...rest } = props;
    return (
      <BaseIcon ref={ref} {...rest}>
        <path
          fill="currentcolor"
          fillOpacity={opacity}
          d="M4.994 3.906c-.256 0-.523.086-.718.281a1.029 1.029 0 0 0 0 1.438l6.28 6.281-6.28 6.281a1.029 1.029 0 0 0 0 1.438c.39.39 1.047.39 1.437 0l6.281-6.28 6.282 6.28c.39.39 1.047.39 1.437 0 .39-.39.39-1.047 0-1.438l-6.281-6.28 6.281-6.282c.39-.39.39-1.047 0-1.438a1.013 1.013 0 0 0-.719-.28c-.256 0-.523.085-.718.28l-6.282 6.281-6.28-6.28a1.013 1.013 0 0 0-.72-.282Z"
        />
      </BaseIcon>
    );
  }
);

CloseIcon.displayName = "CloseIcon";
