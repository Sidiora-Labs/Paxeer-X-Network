import React from "react";
import { BaseIcon, BaseIconProps } from "./baseIcon";

export const ShareIcon = React.forwardRef<SVGSVGElement, BaseIconProps>(
  (props, ref) => {
    const { opacity = 0.54, ...rest } = props;
    return (
      <BaseIcon ref={ref} {...rest}>
        <path
          fill="currentcolor"
          fillOpacity={opacity}
          d="M16.998 1.953a3 3 0 0 0-3 3c0 .257.04.577.102.815L8.747 9.504a3.25 3.25 0 0 0-1.75-.55 3 3 0 1 0 0 6c.633 0 1.257-.236 1.74-.569l5.365 3.766a3.2 3.2 0 0 0-.104.802 3 3 0 1 0 3-3c-.632 0-1.248.228-1.731.562l-5.378-3.763a3.2 3.2 0 0 0 .109-.799c0-.268-.038-.557-.104-.804l5.365-3.752c.479.323 1.118.556 1.739.556a3 3 0 1 0 0-6"
        />
      </BaseIcon>
    );
  }
);

ShareIcon.displayName = "ShareIcon";
