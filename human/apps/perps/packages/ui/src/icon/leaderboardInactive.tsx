import React from "react";
import { BaseIcon, BaseIconProps } from "./baseIcon";

export const LeaderboardInactiveIcon = (props: BaseIconProps) => {
  return (
    <BaseIcon {...props}>
      <path
        d="M18 11H21C22.103 11 23 11.897 23 13V17C23 18.103 22.103 19 21 19H18V17V11Z"
        className="oui-fill-base-contrast-12"
      />
      <path
        d="M6 9V17V19H3C1.897 19 1 18.103 1 17V11C1 9.897 1.897 9 3 9H6Z"
        className="oui-fill-base-contrast-12"
      />
      <path
        d="M16 7V17V19H8V17V7C8 5.897 8.897 5 10 5H14C15.103 5 16 5.897 16 7Z"
        className="oui-fill-base-contrast-36"
      />
    </BaseIcon>
  );
};
