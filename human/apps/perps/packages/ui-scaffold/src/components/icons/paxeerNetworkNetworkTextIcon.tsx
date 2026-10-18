import React from "react";
import type { BaseIconProps } from "@paxeer-network/ui";
export const PaxeerNetworkTextIcon = React.forwardRef<
  SVGSVGElement,
  BaseIconProps
>((props, ref) => (
  <svg
    ref={ref}
    width="116"
    height="14"
    viewBox="0 0 116 14"
    fill="currentColor"
    xmlns="http://www.w3.org/2000/svg"
    role="img"
    aria-label="Paxeer Xtrades"
    {...props}
  >
    <image href="/brand/paxeer-x.png" width="14" height="14" />
    <text
      x="19"
      y="11"
      fontFamily="Arial,sans-serif"
      fontSize="11"
      fontWeight="600"
    >
      Paxeer Xtrades
    </text>
  </svg>
));
