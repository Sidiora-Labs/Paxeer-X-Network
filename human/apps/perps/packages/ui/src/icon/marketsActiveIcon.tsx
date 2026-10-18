import { BaseIcon, BaseIconProps } from "./baseIcon";

export const MarketsActiveIcon = (props: BaseIconProps) => {
  return (
    <BaseIcon {...props}>
      <path
        fillRule="evenodd"
        clipRule="evenodd"
        d="M11.75 8C10.6454 8 9.75 8.89543 9.75 10V18C9.75 19.1046 10.6454 20 11.75 20H13.75C14.8546 20 15.75 19.1046 15.75 18V10C15.75 8.89543 14.8546 8 13.75 8H11.75ZM4.75 12C3.64543 12 2.75 12.8954 2.75 14V18C2.75 19.1046 3.64543 20 4.75 20H6.75C7.85457 20 8.75 19.1046 8.75 18V14C8.75 12.8954 7.85457 12 6.75 12H4.75Z"
        className="oui-fill-base-contrast-16"
      />
      <path
        d="M16.75 6C16.75 4.89543 17.6454 4 18.75 4H20.75C21.8546 4 22.75 4.89543 22.75 6V18C22.75 19.1046 21.8546 20 20.75 20H18.75C17.6454 20 16.75 19.1046 16.75 18V6Z"
        fill="url(#paint0_linear_2097_10887)"
      />
      <defs>
        <linearGradient
          id="paint0_linear_2097_10887"
          x1="22.75"
          y1="12"
          x2="16.75"
          y2="12"
          gradientUnits="userSpaceOnUse"
        >
          <stop stopColor="rgb(var(--oui-gradient-brand-end))" />
          <stop offset="1" stopColor="rgb(var(--oui-gradient-brand-start))" />
        </linearGradient>
      </defs>
    </BaseIcon>
  );
};
