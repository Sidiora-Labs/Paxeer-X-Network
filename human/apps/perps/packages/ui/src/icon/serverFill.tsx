import React from "react";
import { BaseIconProps } from "./baseIcon";
import { BaseIconWithPath } from "./baseIconWithPath";

export const ServerFillIcon = React.forwardRef<SVGSVGElement, BaseIconProps>(
  (props, ref) => {
    return (
      <BaseIconWithPath
        d="M11.998 1.953c-4.362 0-8 1.56-8 4s3.638 4 8 4 8-1.559 8-4-3.638-4-8-4m-8 7.625v2.375c0 2.441 3.638 4 8 4s8-1.559 8-4V9.578c-1.814 1.49-4.642 2.375-8 2.375s-6.186-.885-8-2.375m0 6v2.375c0 2.441 3.638 4 8 4s8-1.559 8-4v-2.375c-1.814 1.49-4.642 2.375-8 2.375s-6.186-.885-8-2.375"
        ref={ref}
        {...props}
      />
    );
  },
);

ServerFillIcon.displayName = "ServerFillIcon";
