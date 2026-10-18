import React, { FC, PropsWithChildren, SVGProps } from "react";
import { cn } from "@paxeer-network/ui";

export const EditIcon: FC<SVGProps<SVGSVGElement>> = (props) => (
  <svg
    width="16"
    height="16"
    viewBox="0 0 16 16"
    fill="currentColor"
    xmlns="http://www.w3.org/2000/svg"
    {...props}
  >
    <mask
      id="a"
      style={{ maskType: "alpha" }}
      maskUnits="userSpaceOnUse"
      x="0"
      y="0"
      width="16"
      height="16"
    >
      <path fill="rgb(var(--oui-color-tertiary))" d="M0 0h16v16H0z" />
    </mask>
    <g mask="url(#a)">
      <path d="M3.333 12.667h.95L10.8 6.15l-.95-.95-6.517 6.517zM2.667 14a.65.65 0 0 1-.475-.192.65.65 0 0 1-.192-.475v-1.616a1.32 1.32 0 0 1 .383-.934l8.417-8.4q.2-.183.442-.283.24-.1.508-.1.267 0 .517.1.249.1.433.3l.917.933q.2.184.291.434a1.44 1.44 0 0 1 0 1.008 1.25 1.25 0 0 1-.291.442l-8.4 8.4a1.32 1.32 0 0 1-.933.383zm7.65-8.317L9.85 5.2l.95.95z" />
    </g>
  </svg>
);

export const TrashIcon: FC<SVGProps<SVGSVGElement>> = (props) => (
  <svg
    width="16"
    height="16"
    viewBox="0 0 16 16"
    fill="currentColor"
    xmlns="http://www.w3.org/2000/svg"
    {...props}
  >
    <path d="M7.205 1.334c-.345 0-.674.136-.918.38L6.001 2H2.667a.667.667 0 1 0 0 1.334h10.667a.667.667 0 1 0 0-1.334h-3.333l-.287-.286a1.3 1.3 0 0 0-.918-.38zM2.911 4.667l1.018 8.842c.088.66.656 1.158 1.322 1.158h5.498c.666 0 1.235-.497 1.323-1.163l1.019-8.837z" />
  </svg>
);

export const AllMarketsIcon: FC<SVGProps<SVGSVGElement>> = (props) => (
  <svg
    width="12"
    height="12"
    viewBox="0 0 12 12"
    fill="currentColor"
    xmlns="http://www.w3.org/2000/svg"
    {...props}
  >
    <mask
      id="a"
      style={{ maskType: "alpha" }}
      maskUnits="userSpaceOnUse"
      x="0"
      y="0"
      width="12"
      height="12"
    >
      <path fill="rgb(var(--oui-color-tertiary))" d="M0 0h12v12H0z" />
    </mask>
    <g mask="url(#a)">
      <path d="M8.5 10a.48.48 0 0 1-.356-.144A.48.48 0 0 1 8 9.5V7q0-.213.144-.356A.48.48 0 0 1 8.5 6.5h1q.212 0 .356.144A.48.48 0 0 1 10 7v2.5q0 .212-.144.356A.48.48 0 0 1 9.5 10zm-3 0a.48.48 0 0 1-.356-.144A.48.48 0 0 1 5 9.5v-7q0-.212.144-.356A.48.48 0 0 1 5.5 2h1q.213 0 .356.144A.48.48 0 0 1 7 2.5v7q0 .212-.144.356A.48.48 0 0 1 6.5 10zm-3 0a.48.48 0 0 1-.356-.144A.48.48 0 0 1 2 9.5V5q0-.213.144-.356A.48.48 0 0 1 2.5 4.5h1q.212 0 .356.144A.48.48 0 0 1 4 5v4.5q0 .212-.144.356A.48.48 0 0 1 3.5 10z" />
    </g>
  </svg>
);

export const NewListingsIcon: FC<SVGProps<SVGSVGElement>> = (props) => (
  <svg
    width="12"
    height="12"
    viewBox="0 0 12 12"
    fill="currentColor"
    xmlns="http://www.w3.org/2000/svg"
    {...props}
  >
    <path d="M5.993.958a.416.416 0 0 0-.41.422v.416a.416.416 0 1 0 .832 0V1.38a.416.416 0 0 0-.422-.422M2.749 2.29a.416.416 0 0 0-.29.714l.291.292a.416.416 0 1 0 .589-.588l-.292-.292a.42.42 0 0 0-.298-.126m6.489 0a.42.42 0 0 0-.286.126l-.292.292a.416.416 0 1 0 .588.588l.292-.292a.416.416 0 0 0-.302-.714m-3.239.753a2.895 2.895 0 0 0-2.913 2.914A2.86 2.86 0 0 0 4.751 8.58v1.123c0 .458.374.833.832.833h.832a.835.835 0 0 0 .833-.833V8.58a2.86 2.86 0 0 0 1.665-2.622 2.895 2.895 0 0 0-2.914-2.914M1.421 5.541a.416.416 0 1 0 0 .833h.417a.416.416 0 1 0 0-.833zm8.74 0a.416.416 0 1 0 0 .833h.416a.417.417 0 1 0 0-.833zM3.037 8.492a.42.42 0 0 0-.287.126l-.291.292a.416.416 0 1 0 .588.588l.292-.292a.416.416 0 0 0-.302-.714m5.913 0a.416.416 0 0 0-.29.714l.292.292a.416.416 0 1 0 .588-.588l-.292-.292a.42.42 0 0 0-.298-.126" />
  </svg>
);

export const FavoritesIcon: FC<SVGProps<SVGSVGElement>> = (props) => (
  <svg
    width="20"
    height="21"
    viewBox="0 0 20 21"
    fill="currentColor"
    xmlns="http://www.w3.org/2000/svg"
    {...props}
    // The color of the favorite icon does not change with the theme.
    className={cn("oui-text-[rgba(255,154,46,1)]", props.className)}
  >
    <mask
      id="a"
      style={{ maskType: "alpha" }}
      maskUnits="userSpaceOnUse"
      x="0"
      y="0"
      width="20"
      height="21"
    >
      <path fill="#D9D9D9" d="M0 .5h20v20H0z" />
    </mask>
    <g mask="url(#a)">
      <path d="m10 14.074-3.2 1.913a.6.6 0 0 1-.332.068.6.6 0 0 1-.277-.101.5.5 0 0 1-.186-.256.5.5 0 0 1-.005-.336l.84-3.556-2.82-2.394a.5.5 0 0 1-.174-.281.6.6 0 0 1 .013-.315.5.5 0 0 1 .173-.252.55.55 0 0 1 .305-.112l3.693-.33 1.467-3.393a.57.57 0 0 1 .211-.255A.54.54 0 0 1 10 4.39q.16 0 .292.083.131.082.211.255l1.467 3.414 3.693.309q.178.014.305.123.126.11.173.262t.002.304a.56.56 0 0 1-.183.27l-2.8 2.395.84 3.556a.5.5 0 0 1-.005.336.5.5 0 0 1-.186.256.6.6 0 0 1-.277.101.6.6 0 0 1-.332-.068z" />
    </g>
  </svg>
);

export const UnFavoritesIcon: FC<SVGProps<SVGSVGElement>> = (props) => (
  <svg
    width="20"
    height="21"
    viewBox="0 0 20 21"
    fill="currentColor"
    xmlns="http://www.w3.org/2000/svg"
    {...props}
    // The color of the favorite icon does not change with the theme.
    className={cn(
      "oui-text-base-contrast-36 hover:oui-text-[rgba(255,154,46,1)]",
      props.className,
    )}
  >
    <mask
      id="a"
      style={{ maskType: "alpha" }}
      maskUnits="userSpaceOnUse"
      x="0"
      y="0"
      width="20"
      height="21"
    >
      <path fill="#D9D9D9" d="M0 .5h20v20H0z" />
    </mask>
    <g mask="url(#a)">
      <path d="M7.333 14.396 10 12.813l2.688 1.583-.709-3 2.313-1.98-3.063-.27L10 6.292 8.77 9.146l-3.062.27 2.334 1.98zM10 14.074l-3.2 1.913a.6.6 0 0 1-.332.068.6.6 0 0 1-.277-.101.5.5 0 0 1-.186-.256.5.5 0 0 1-.005-.336l.84-3.556-2.82-2.394a.5.5 0 0 1-.174-.281.6.6 0 0 1 .013-.315.5.5 0 0 1 .173-.252.55.55 0 0 1 .305-.112l3.693-.33 1.467-3.393a.57.57 0 0 1 .211-.255A.54.54 0 0 1 10 4.39q.16 0 .292.083.131.082.211.255l1.467 3.414 3.693.309q.178.014.305.123.126.11.173.262t.002.304a.56.56 0 0 1-.183.27l-2.8 2.395.84 3.556a.5.5 0 0 1-.005.336.5.5 0 0 1-.186.256.6.6 0 0 1-.277.101.6.6 0 0 1-.332-.068z" />
    </g>
  </svg>
);

export const CirclePlusIcon: FC<SVGProps<SVGSVGElement>> = (props) => (
  <svg
    width="18"
    height="18"
    viewBox="0 0 18 18"
    fill="currentColor"
    xmlns="http://www.w3.org/2000/svg"
    {...props}
  >
    <mask
      id="a"
      style={{ maskType: "alpha" }}
      maskUnits="userSpaceOnUse"
      x="0"
      y="0"
      width="18"
      height="18"
    >
      <path fill="rgb(var(--oui-color-tertiary))" d="M0 0h18v18H0z" />
    </mask>
    <g mask="url(#a)">
      <path d="M8.325 12.6h1.35V9.675H12.6v-1.35H9.675V5.4h-1.35v2.925H5.4v1.35h2.925zm.68 3.6a7 7 0 0 1-2.799-.562A7.3 7.3 0 0 1 3.91 14.09a7.3 7.3 0 0 1-1.546-2.296A7 7 0 0 1 1.8 8.99q0-1.49.563-2.794a7.26 7.26 0 0 1 3.843-3.834A7 7 0 0 1 9.009 1.8q1.49 0 2.794.562 1.303.563 2.288 1.547a7.3 7.3 0 0 1 1.547 2.292q.561 1.308.562 2.794a7 7 0 0 1-.562 2.799 7.3 7.3 0 0 1-1.547 2.297 7.3 7.3 0 0 1-2.292 1.547 7 7 0 0 1-2.794.562M9 14.85q2.437 0 4.144-1.706Q14.85 11.437 14.85 9t-1.706-4.144Q11.437 3.15 9 3.15T4.856 4.856 3.15 9t1.706 4.144Q6.563 14.85 9 14.85" />
    </g>
  </svg>
);

export const SearchIcon: FC<SVGProps<SVGSVGElement>> = (props) => (
  <svg
    width="14"
    height="14"
    viewBox="0 0 14 14"
    fill="currentColor"
    xmlns="http://www.w3.org/2000/svg"
    {...props}
  >
    <path d="M5.841 1.14a4.667 4.667 0 0 0 0 9.333 4.74 4.74 0 0 0 2.875-.975l2.54 2.56a.6.6 0 0 0 .838 0 .6.6 0 0 0 0-.838L9.537 8.677a4.72 4.72 0 0 0 .971-2.871 4.667 4.667 0 0 0-4.667-4.667m0 1.166a3.5 3.5 0 1 1 0 7 3.5 3.5 0 0 1 0-7" />
  </svg>
);

export const MoveToTopIcon: FC<SVGProps<SVGSVGElement>> = (props) => (
  <svg
    width="20"
    height="21"
    viewBox="0 0 20 21"
    fill="currentColor"
    xmlns="http://www.w3.org/2000/svg"
    {...props}
  >
    <path d="M10 5.5a.76.76 0 0 0-.583.25l-4.334 4c0 .083-.083.25-.083.333 0 .25.167.417.417.417h2.916v6.667c0 .416.334.833.834.833h1.666c.5 0 .834-.417.834-.833V10.5h2.916c.25 0 .417-.167.417-.417 0-.083-.083-.25-.083-.333l-4.25-4.084c-.25-.083-.417-.166-.667-.166M2.177 3.06A.8.8 0 0 1 2.5 3h15a.834.834 0 1 1 0 1.667h-15a.833.833 0 0 1-.323-1.607" />
  </svg>
);

export const PaxeerNetworkIcon: FC<SVGProps<SVGSVGElement>> = (props) => (
  <svg
    width="12"
    height="13"
    viewBox="0 0 12 13"
    fill="currentColor"
    xmlns="http://www.w3.org/2000/svg"
    {...props}
  >
    <path
      fillRule="evenodd"
      clipRule="evenodd"
      d="M6.015.34h-.029a5.98 5.98 0 0 0-3.93 1.477c-.074.065-.027.184.072.184h7.745c.099 0 .146-.119.072-.184A5.98 5.98 0 0 0 6.015.341M3.48 7.866a.23.23 0 0 1 .187.1A2.85 2.85 0 0 0 6 9.178a2.85 2.85 0 0 0 2.334-1.213.23.23 0 0 1 .186-.1h3.104c.09 0 .155.086.13.172A6 6 0 0 1 6 12.327a6 6 0 0 1-5.755-4.29.134.134 0 0 1 .13-.172zM8.26 4.6a.29.29 0 0 0 .229.116h3.11c.09 0 .156-.086.13-.173a6 6 0 0 0-1.106-2.03.22.22 0 0 0-.165-.077H1.543a.22.22 0 0 0-.165.077 6 6 0 0 0-1.105 2.03.134.134 0 0 0 .13.173h3.11A.29.29 0 0 0 3.74 4.6 2.85 2.85 0 0 1 6 3.488c.92 0 1.738.436 2.26 1.112m.414 2.717c-.02.055.02.115.079.115h3.033a.135.135 0 0 0 .133-.112 6 6 0 0 0-.015-2.058.135.135 0 0 0-.133-.11H8.72c-.06 0-.1.061-.078.117a2.83 2.83 0 0 1 .032 2.048m-5.427.115c.06 0 .1-.06.079-.115a2.84 2.84 0 0 1 .032-2.048.085.085 0 0 0-.078-.118H.23a.135.135 0 0 0-.134.11A6 6 0 0 0 .08 7.32c.01.065.067.112.133.112z"
    />
  </svg>
);

export const FavoritesIcon2: FC<SVGProps<SVGSVGElement>> = (props) => (
  <svg
    width="12"
    height="13"
    viewBox="0 0 12 13"
    fill="currentColor"
    xmlns="http://www.w3.org/2000/svg"
    {...props}
    // The color of the favorite icon does not change with the theme.
    className={cn("oui-text-[rgba(255,154,46,1)]", props.className)}
  >
    <path d="M6.012 1.72c-.286 0-.577.178-.75.532L4.2 4.438l-2.42.344c-.78.108-1.03.872-.47 1.42l1.75 1.703-.407 2.39c-.133.773.508 1.24 1.203.874.268-.142 1.648-.857 2.155-1.124l2.155 1.124c.695.367 1.339-.1 1.202-.874l-.421-2.39 1.749-1.702c.565-.547.326-1.31-.453-1.421l-2.436-.344L6.76 2.252c-.172-.354-.464-.531-.75-.531" />
  </svg>
);

export const UnFavoritesIcon2: FC<SVGProps<SVGSVGElement>> = (props) => (
  <svg
    width="12"
    height="13"
    viewBox="0 0 12 13"
    fill="currentColor"
    xmlns="http://www.w3.org/2000/svg"
    {...props}
    // The color of the favorite icon does not change with the theme.
    className={cn(
      "oui-text-base-contrast-36 hover:oui-text-[rgba(255,154,46,1)]",
      props.className,
    )}
  >
    <path d="M6.012 1.72c-.286 0-.577.178-.75.532L4.2 4.438l-2.42.344c-.78.108-1.03.872-.47 1.42l1.75 1.703-.407 2.39c-.133.773.508 1.24 1.203.874.268-.142 1.648-.857 2.155-1.124l2.155 1.124c.695.367 1.339-.1 1.202-.874l-.421-2.39 1.749-1.702c.565-.547.326-1.31-.453-1.421l-2.436-.344L6.76 2.252c-.172-.354-.464-.531-.75-.531m0 1.297 1.03 2.108c.073.15.211.242.375.266l2.343.343-1.702 1.64a.48.48 0 0 0-.14.437l.405 2.312L6.246 9.03a.5.5 0 0 0-.468 0L3.7 10.123l.39-2.296a.52.52 0 0 0-.14-.453l-1.687-1.64 2.327-.328a.52.52 0 0 0 .39-.28z" />
  </svg>
);

export const TopIcon: FC<SVGProps<SVGSVGElement>> = (props) => (
  <svg
    width="16"
    height="17"
    viewBox="0 0 16 17"
    fill="currentColor"
    xmlns="http://www.w3.org/2000/svg"
    {...props}
  >
    <path d="M2.01 2.915a.667.667 0 1 0 0 1.334h12a.667.667 0 0 0 0-1.334zM7.343 13.59a.667.667 0 0 0 1.333 0V7.88l2 1.98.937-.938L8.49 5.777a.686.686 0 0 0-.959 0L4.405 8.923l.938.937 2-1.979z" />
  </svg>
);

export const DeleteIcon: FC<SVGProps<SVGSVGElement>> = (props) => (
  <svg
    width="16"
    height="17"
    viewBox="0 0 16 17"
    fill="currentColor"
    xmlns="http://www.w3.org/2000/svg"
    {...props}
  >
    <path d="M4.67 2.203A2.667 2.667 0 0 0 2.005 4.87v6.667a2.667 2.667 0 0 0 2.667 2.666h6.666a2.667 2.667 0 0 0 2.667-2.666V4.87a2.667 2.667 0 0 0-2.667-2.667zm1.334 3.334c.17 0 .349.057.48.187l1.52 1.52 1.52-1.52a.68.68 0 0 1 .48-.187c.17 0 .349.057.48.187.26.26.26.698 0 .959l-1.521 1.52 1.52 1.52c.26.262.26.699 0 .96a.687.687 0 0 1-.958 0L8.004 9.162l-1.52 1.52a.687.687 0 0 1-.96 0 .687.687 0 0 1 0-.958l1.521-1.52-1.52-1.521a.687.687 0 0 1 0-.96.68.68 0 0 1 .479-.186" />
  </svg>
);

export const AddIcon: FC<SVGProps<SVGSVGElement>> = (props) => (
  <svg
    width="16"
    height="16"
    viewBox="0 0 16 16"
    fill="currentColor"
    xmlns="http://www.w3.org/2000/svg"
    {...props}
  >
    <mask
      id="a"
      style={{ maskType: "alpha" }}
      maskUnits="userSpaceOnUse"
      x="0"
      y="0"
      width="16"
      height="16"
    >
      <path d="M0 0h16v16H0z" />
    </mask>
    <g mask="url(#a)">
      <path d="M7.333 8.667h-4V7.333h4v-4h1.333v4h4v1.334h-4v4H7.333z" />
    </g>
  </svg>
);

export const ActiveAddIcon: FC<SVGProps<SVGSVGElement>> = (props) => (
  <svg
    xmlns="http://www.w3.org/2000/svg"
    width="12"
    height="12"
    viewBox="0 0 12 12"
    fill="none"
  >
    <path
      d="M5.99536 1.46487C5.71936 1.46487 5.49536 1.68887 5.49536 1.96487V5.46487H1.99536C1.71936 5.46487 1.49536 5.68887 1.49536 5.96487C1.49536 6.24087 1.71936 6.46487 1.99536 6.46487H5.49536V9.96487C5.49536 10.2409 5.71936 10.4649 5.99536 10.4649C6.27136 10.4649 6.49536 10.2409 6.49536 9.96487V6.46487H9.99536C10.2714 6.46487 10.4954 6.24087 10.4954 5.96487C10.4954 5.68887 10.2714 5.46487 9.99536 5.46487H6.49536V1.96487C6.49536 1.68887 6.27136 1.46487 5.99536 1.46487Z"
      fill="url(#paint0_linear_1951_55156)"
    />
    <defs>
      <linearGradient
        id="paint0_linear_1951_55156"
        x1="10.4954"
        y1="5.96487"
        x2="1.49536"
        y2="5.96487"
        gradientUnits="userSpaceOnUse"
      >
        <stop stopColor="rgb(var(--oui-gradient-brand-end))" />
        <stop offset="1" stopColor="rgb(var(--oui-gradient-brand-start))" />
      </linearGradient>
    </defs>
  </svg>
);

// export const ExpandIcon: FC<SVGProps<SVGSVGElement>> = (props) => (
//   <svg
//     width="16"
//     height="16"
//     viewBox="0 0 16 16"
//     fill="currentColor"
//     xmlns="http://www.w3.org/2000/svg"
//     {...props}
//   >
//     <path d="M6.326 8.826a.84.84 0 0 0-.6.234L2.16 12.627v-2.135H.492v4.167c0 .46.373.833.834.833h4.166v-1.667H3.357l3.567-3.567a.857.857 0 0 0 0-1.198.84.84 0 0 0-.598-.234M10.502.492V2.16h2.135L9.07 5.726a.857.857 0 0 0 0 1.199.86.86 0 0 0 1.197 0l3.568-3.568v2.135h1.667V1.326a.834.834 0 0 0-.834-.834z" />
//   </svg>
// );

// export const CollapseIcon: FC<SVGProps<SVGSVGElement>> = (props) => (
//   <svg
//     width="16"
//     height="16"
//     viewBox="0 0 16 16"
//     fill="currentColor"
//     xmlns="http://www.w3.org/2000/svg"
//     {...props}
//   >
//     <path d="M14.668.492a.85.85 0 0 0-.599.234l-3.567 3.568V2.159H8.835v4.167c0 .46.373.833.833.833h4.167V5.492H11.7l3.569-3.567a.86.86 0 0 0 0-1.199.85.85 0 0 0-.6-.234m-12.5 8.334v1.666h2.135L.736 14.06a.86.86 0 0 0 0 1.198.86.86 0 0 0 1.198 0l3.568-3.567v2.134h1.666V9.66a.834.834 0 0 0-.833-.833z" />
//   </svg>
// );

export const FilterIcon: FC<SVGProps<SVGSVGElement>> = (props) => (
  <svg
    width="18"
    height="18"
    viewBox="0 0 18 18"
    fill="currentColor"
    xmlns="http://www.w3.org/2000/svg"
    {...props}
  >
    <path d="M11.2422 2.19751C10.2913 2.19751 9.40469 2.82152 9.12801 3.69902L2.99219 3.69751C2.57796 3.69751 2.24219 4.03351 2.24219 4.44751C2.24219 4.86151 2.57796 5.19751 2.99219 5.19751L9.12801 5.19678C9.46409 6.11553 10.2913 6.69751 11.2422 6.69751C12.193 6.69751 13.0288 6.11403 13.3707 5.19078L14.9922 5.19751C15.4064 5.19751 15.7422 4.86151 15.7422 4.44751C15.7422 4.03351 15.4064 3.69751 14.9922 3.69751H13.3643C13.0032 2.78326 12.193 2.19751 11.2422 2.19751ZM11.2422 3.69751C11.6564 3.69751 11.9922 4.03351 11.9922 4.44751C11.9922 4.86151 11.6564 5.19751 11.2422 5.19751C10.828 5.19751 10.4922 4.86151 10.4922 4.44751C10.4922 4.03351 10.828 3.69751 11.2422 3.69751ZM6.74219 6.69751C5.75369 6.69751 4.92629 7.31252 4.62419 8.19902C4.51874 8.20577 2.99219 8.19751 2.99219 8.19751C2.57796 8.19751 2.24219 8.53351 2.24219 8.94751C2.24219 9.36151 2.57796 9.69751 2.99219 9.69751C2.99219 9.69751 4.53149 9.68176 4.62741 9.69151C4.92951 10.578 5.75369 11.1975 6.74219 11.1975C7.69304 11.1975 8.50784 10.6148 8.86424 9.69975L14.9922 9.69751C15.4064 9.69751 15.7422 9.36151 15.7422 8.94751C15.7422 8.53351 15.4064 8.19751 14.9922 8.19751L8.86034 8.19078C8.53161 7.29603 7.69304 6.69751 6.74219 6.69751ZM6.74219 8.19751C7.15641 8.19751 7.49219 8.53351 7.49219 8.94751C7.49219 9.36151 7.15641 9.69751 6.74219 9.69751C6.32796 9.69751 5.99219 9.36151 5.99219 8.94751C5.99219 8.53351 6.32796 8.19751 6.74219 8.19751ZM11.2422 11.1975C10.2913 11.1975 9.42157 11.8238 9.12749 12.69L2.99219 12.6975C2.57796 12.6975 2.24219 13.0335 2.24219 13.4475C2.24219 13.8615 2.57796 14.1975 2.99219 14.1975L9.11639 14.1982C9.42756 15.087 10.2913 15.6975 11.2422 15.6975C12.193 15.6975 13.0129 15.1027 13.3741 14.1982L14.9922 14.1975C15.4064 14.1975 15.7422 13.8615 15.7422 13.4475C15.7422 13.0335 15.4064 12.6975 14.9922 12.6975L13.3672 12.693C13.0435 11.8058 12.193 11.1975 11.2422 11.1975ZM11.2422 12.6975C11.6564 12.6975 11.9922 13.0335 11.9922 13.4475C11.9922 13.8615 11.6564 14.1975 11.2422 14.1975C10.828 14.1975 10.4922 13.8615 10.4922 13.4475C10.4922 13.0335 10.828 12.6975 11.2422 12.6975Z" />
  </svg>
);

export const TriangleDownIcon: FC<SVGProps<SVGSVGElement>> = (props) => (
  <svg
    width="12"
    height="12"
    viewBox="0 0 12 12"
    fill="currentColor"
    xmlns="http://www.w3.org/2000/svg"
    {...props}
  >
    <path d="M3.003 3.998a.494.494 0 0 0-.39.797l3 4c.2.266.597.266.797 0l3-4a.5.5 0 0 0-.407-.797z" />
  </svg>
);

export const ArrowLeftIcon: FC<SVGProps<SVGSVGElement>> = (props) => (
  <svg
    width="16"
    height="16"
    viewBox="0 0 16 16"
    fill="currentColor"
    xmlns="http://www.w3.org/2000/svg"
    {...props}
  >
    <path d="M9.186 3.348a.67.67 0 0 0-.436.27l-2.657 4a.69.69 0 0 0 0 .75l2.657 4a.68.68 0 0 0 .934.188.685.685 0 0 0 .187-.937L7.463 7.993 9.87 4.37a.685.685 0 0 0-.187-.938.65.65 0 0 0-.498-.083" />
  </svg>
);

export const ArrowRightIcon: FC<SVGProps<SVGSVGElement>> = (props) => (
  <svg
    width="16"
    height="16"
    viewBox="0 0 16 16"
    fill="currentColor"
    xmlns="http://www.w3.org/2000/svg"
    {...props}
  >
    <path d="M6.777 3.348a.65.65 0 0 0-.498.083.685.685 0 0 0-.187.938L8.5 7.993l-2.408 3.625a.685.685 0 0 0 .187.938.68.68 0 0 0 .934-.187l2.657-4a.69.69 0 0 0 0-.75l-2.657-4a.67.67 0 0 0-.436-.271" />
  </svg>
);

const BaseSortIcon = (props: PropsWithChildren) => {
  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      width="10"
      height="10"
      viewBox="0 0 10 10"
      fill="none"
    >
      {props.children}
    </svg>
  );
};

export const SortingIcon = () => {
  return (
    <BaseSortIcon>
      <path
        d="M5 1.042a.47.47 0 0 0-.338.135L2.166 3.844c-.206.22-.005.531.338.531h4.992c.342 0 .543-.311.337-.531L5.338 1.177A.47.47 0 0 0 5 1.042m0 7.916a.47.47 0 0 1-.338-.135L2.166 6.156c-.206-.22-.005-.531.338-.531h4.992c.342 0 .543.311.337.531L5.338 8.823A.47.47 0 0 1 5 8.958"
        fill="rgb(var(--oui-color-base-foreground) / 0.2)"
      />
    </BaseSortIcon>
  );
};

export const AscendingIcon = () => {
  return (
    <BaseSortIcon>
      <path
        d="M5 1.042a.47.47 0 0 0-.338.135L2.166 3.844c-.206.22-.005.531.338.531h4.992c.342 0 .543-.311.337-.531L5.338 1.177A.47.47 0 0 0 5 1.042"
        fill="rgb(var(--oui-color-base-foreground)/0.8)"
      />
      <path
        d="M5 8.958a.47.47 0 0 1-.338-.135L2.166 6.156c-.206-.22-.005-.531.338-.531h4.992c.342 0 .543.311.337.531L5.338 8.823A.47.47 0 0 1 5 8.958"
        fill="rgb(var(--oui-color-base-foreground)/0.2)"
      />
    </BaseSortIcon>
  );
};

export const DescendingIcon = () => {
  return (
    <BaseSortIcon>
      <path
        d="M5 1.042a.47.47 0 0 0-.338.135L2.166 3.844c-.206.22-.005.531.338.531h4.992c.342 0 .543-.311.337-.531L5.338 1.177A.47.47 0 0 0 5 1.042"
        fill="rgb(var(--oui-color-base-foreground)/0.2)"
      />
      <path
        d="M5 8.958a.47.47 0 0 1-.338-.135L2.166 6.156c-.206-.22-.005-.531.338-.531h4.992c.342 0 .543.311.337.531L5.338 8.823A.47.47 0 0 1 5 8.958"
        fill="rgb(var(--oui-color-base-foreground)/0.8)"
      />
    </BaseSortIcon>
  );
};
