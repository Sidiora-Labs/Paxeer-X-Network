import React from "react";
import { BaseIcon, BaseIconProps } from "./baseIcon";

export const EyeIcon = React.forwardRef<SVGSVGElement, BaseIconProps>(
  (props, ref) => {
    const { opacity = 0.54, ...rest } = props;
    return (
      <BaseIcon ref={ref} {...rest}>
        <path
          fill="currentcolor"
          fillOpacity={opacity}
          d="M12.001 3.998c-2.959 0-5.452 1.454-7.5 3.844a16.652 16.652 0 00-1.812 2.562c-.302.528-.486.936-.594 1.188a1.058 1.058 0 000 .812c.108.252.292.66.594 1.188a16.652 16.652 0 001.812 2.562c2.048 2.39 4.541 3.844 7.5 3.844 2.96 0 5.452-1.454 7.5-3.844a16.652 16.652 0 001.812-2.562c.302-.528.486-.936.594-1.188a1.058 1.058 0 000-.812c-.108-.252-.292-.66-.594-1.188a16.652 16.652 0 00-1.812-2.562c-2.048-2.39-4.54-3.844-7.5-3.844zm0 4a4 4 0 110 8 4 4 0 010-8zm0 2a2 2 0 100 3.999 2 2 0 000-4z"
        />
      </BaseIcon>
    );
  }
);

EyeIcon.displayName = "EyeIcon";
