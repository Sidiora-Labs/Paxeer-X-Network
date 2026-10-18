import React from "react";
import { BaseIcon, BaseIconProps } from "./baseIcon";

export const NewsFillIcon = React.forwardRef<SVGSVGElement, BaseIconProps>(
  (props, ref) => {
    const { opacity = 1, ...rest } = props;
    return (
      <BaseIcon ref={ref} {...rest}>
        <path
          fill="currentcolor"
          fillOpacity={opacity}
          d="M12 2.75a1 1 0 0 1 1 1v1a5 5 0 0 1 5 5v5c0 .577.245 1.098.637 1.463.606.565 1.363 1.21 1.363 2.037a1.5 1.5 0 0 1-1.5 1.5h-4.05q.049.243.05.5a2.5 2.5 0 1 1-4.95-.5H5.5a1.5 1.5 0 0 1-1.5-1.5c0-.828.757-1.472 1.363-2.037.392-.365.637-.886.637-1.463v-5a5 5 0 0 1 5-5v-1a1 1 0 0 1 1-1m-1.146 17a1.25 1.25 0 1 0 2.292 0z"
        />
      </BaseIcon>
    );
  },
);

NewsFillIcon.displayName = "NewsFillIcon";
