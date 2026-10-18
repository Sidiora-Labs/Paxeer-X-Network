import type React from "react";
import type { SVGProps } from "react";

/**
 * Order-entry column icon used inside the advanced layout cards.
 * Copied from trading SwitchLayout to keep visual style consistent.
 */
export const OrderEntryIcon: React.FC<SVGProps<SVGSVGElement>> = (props) => (
  <svg
    width="36"
    height="84"
    viewBox="0 0 36 84"
    fill="none"
    xmlns="http://www.w3.org/2000/svg"
    {...props}
  >
    <rect width="36" height="17" rx="2" fill="rgb(var(--oui-color-base-7))" />
    <rect
      y="19"
      width="36"
      height="54"
      rx="2"
      fill="rgb(var(--oui-color-base-7))"
    />
    <rect
      y="75"
      width="36"
      height="9"
      rx="2"
      fill="rgb(var(--oui-color-base-7))"
    />
    <rect
      x="3"
      y="79"
      width="30"
      height="1"
      rx="0.5"
      fill="url(#paint0_linear_17647_26849)"
    />
    <rect
      x="3"
      y="22"
      width="14"
      height="6"
      rx="2"
      fill="rgb(var(--oui-color-success-darken))"
    />
    <rect
      x="19"
      y="22"
      width="14"
      height="6"
      rx="2"
      fill="rgb(var(--oui-color-danger-darken))"
    />
    <rect
      x="3"
      y="11"
      width="14"
      height="3"
      rx="1.5"
      fill="rgb(var(--oui-color-base-4))"
    />
    <rect
      x="19"
      y="11"
      width="14"
      height="3"
      rx="1.5"
      fill="rgb(var(--oui-color-primary-darken))"
    />
    <rect
      x="3"
      y="62"
      width="30"
      height="8"
      rx="2"
      fill="rgb(var(--oui-color-success-darken))"
    />
    <defs>
      <linearGradient
        id="paint0_linear_17647_26849"
        x1="33"
        y1="79.5"
        x2="3"
        y2="79.5"
        gradientUnits="userSpaceOnUse"
      >
        <stop stopColor="rgb(var(--oui-color-primary-light))" />
        <stop offset="1" stopColor="rgb(var(--oui-gradient-brand-start))" />
      </linearGradient>
    </defs>
  </svg>
);
