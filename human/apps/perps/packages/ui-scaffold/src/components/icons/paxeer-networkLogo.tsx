import type { FC, SVGProps } from "react";
export const PaxeerNetworkLogo: FC<SVGProps<SVGSVGElement>> = (props) => (
  <svg
    width="188"
    height="32"
    viewBox="0 0 188 32"
    xmlns="http://www.w3.org/2000/svg"
    role="img"
    aria-label="Paxeer Xtrades"
    {...props}
  >
    <image href="/brand/paxeer-x.png" width="32" height="32" />
    <text
      x="41"
      y="22"
      fill="currentColor"
      fontFamily="Arial,sans-serif"
      fontSize="17"
      fontWeight="600"
    >
      Paxeer Xtrades
    </text>
  </svg>
);
