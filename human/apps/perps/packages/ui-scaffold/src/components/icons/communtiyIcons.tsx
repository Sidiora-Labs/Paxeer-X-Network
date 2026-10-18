import React from "react";
import { BaseIconProps } from "@paxeer-network/ui";

export const CommuntiyTelegramIcon = React.forwardRef<
  SVGSVGElement,
  BaseIconProps
>((props, ref) => {
  const { size = 20, ...rest } = props;
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 20 20"
      ref={ref}
      fill="currentColor"
      xmlns="http://www.w3.org/2000/svg"
      className="oui-fill-base-contrast-54"
      {...rest}
    >
      <path
        d="M4.108 9.464S9.645 7.13 11.565 6.31c.736-.328 3.233-1.38 3.233-1.38s1.152-.46 1.056.658c-.032.46-.288 2.069-.544 3.81-.384 2.463-.8 5.157-.8 5.157s-.064.755-.608.887c-.544.13-1.44-.46-1.6-.592-.129-.098-2.401-1.576-3.233-2.299-.224-.197-.48-.591.032-1.051a124 124 0 0 0 3.36-3.285c.384-.394.768-1.313-.832-.197-2.272 1.61-4.513 3.12-4.513 3.12s-.512.33-1.472.034-2.08-.69-2.08-.69-.768-.493.544-1.018"
        fill="currentColor"
      />
    </svg>
  );
});
export const CommuntiyDiscordIcon = React.forwardRef<
  SVGSVGElement,
  BaseIconProps
>((props, ref) => {
  const { size = 20, ...rest } = props;
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 20 20"
      ref={ref}
      fill="currentColor"
      xmlns="http://www.w3.org/2000/svg"
      className="oui-fill-base-contrast-54"
      {...rest}
    >
      <path
        d="M14.956 5.522c1.604 2.374 2.396 5.053 2.1 8.136a.05.05 0 0 1-.02.033 11.7 11.7 0 0 1-3.55 1.805.045.045 0 0 1-.05-.017 9.6 9.6 0 0 1-.725-1.188.046.046 0 0 1 .024-.063 7 7 0 0 0 1.108-.531.046.046 0 0 0 .004-.076 6 6 0 0 1-.22-.174.04.04 0 0 0-.047-.006c-2.296 1.068-4.812 1.068-7.136 0a.04.04 0 0 0-.046.006 6 6 0 0 1-.22.174.046.046 0 0 0 .005.076c.353.204.721.384 1.107.531.025.01.037.039.025.063a8.5 8.5 0 0 1-.725 1.188.05.05 0 0 1-.05.017 11.7 11.7 0 0 1-3.546-1.805.05.05 0 0 1-.018-.033c-.248-2.667.257-5.368 2.097-8.137a.04.04 0 0 1 .02-.016 11.6 11.6 0 0 1 2.89-.903.05.05 0 0 1 .046.022c.125.224.269.51.366.744a10.7 10.7 0 0 1 3.246 0 8 8 0 0 1 .36-.744.044.044 0 0 1 .046-.022 11.7 11.7 0 0 1 2.89.903q.012.005.019.017m-6.018 5.07c.011-.788-.56-1.44-1.276-1.44-.71 0-1.276.647-1.276 1.44 0 .795.576 1.442 1.276 1.442.71 0 1.276-.647 1.276-1.441m4.718 0c.011-.788-.56-1.44-1.276-1.44-.71 0-1.276.647-1.276 1.44 0 .795.577 1.442 1.276 1.442.717 0 1.276-.647 1.276-1.441"
        fill="currentColor"
      />
    </svg>
  );
});
export const CommuntiyXIcon = React.forwardRef<SVGSVGElement, BaseIconProps>(
  (props, ref) => {
    const { size = 20, ...rest } = props;
    return (
      <svg
        width={size}
        height={size}
        viewBox="0 0 20 20"
        ref={ref}
        fill="currentColor"
        xmlns="http://www.w3.org/2000/svg"
        className="oui-fill-base-contrast-54"
        {...rest}
      >
        <path
          d="m4.42 4.73 4.633 6.194-4.662 5.037H5.44l4.082-4.41 3.298 4.41h3.57l-4.893-6.543 4.34-4.689h-1.05l-3.759 4.062-3.037-4.062zm1.543.772h1.64l7.244 9.686h-1.64z"
          fill="currentColor"
        />
      </svg>
    );
  },
);

CommuntiyXIcon.displayName = "CommuntiyXIcon";
CommuntiyTelegramIcon.displayName = "CommuntiyTelegramIcon";
CommuntiyDiscordIcon.displayName = "CommuntiyDiscordIcon";
