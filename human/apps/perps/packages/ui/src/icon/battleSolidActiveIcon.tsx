import React from "react";
import { BaseIcon, BaseIconProps } from "./baseIcon";

export const BattleSolidActiveIcon = (props: BaseIconProps) => {
  return (
    <BaseIcon {...props}>
      <path
        d="M2.66095 2.5L3.59576 6.17025L9.63192 11.7473L7.37886 14.095L6.59375 13.3208L5.27186 14.6183L6.10079 15.4319L3.75644 17.8728L3.32189 17.4498L2 18.7473L4.80446 21.5L6.12635 20.2025L5.6918 19.776L8.18222 17.4785L9.01114 18.2885L10.333 16.991L9.54793 16.2204L12.0091 13.9409L14.463 16.2097L13.667 16.991L14.9889 18.2885L15.8251 17.4677L18.3155 19.7652L17.87 20.2025L19.1955 21.5L22 18.7473L20.6781 17.4498L20.2545 17.8656L17.9102 15.4211L18.7281 14.6183L17.4062 13.3208L16.6284 14.0842L14.3863 11.7473L14.39 11.7437L12.0091 9.26702L6.40022 3.41756L2.66095 2.5ZM21.3573 2.5L17.618 3.41756L13.0864 8.14158L15.606 10.6183L20.4225 6.17025L21.3573 2.5Z"
        fill="url(#paint0_linear_battle_solid_active)"
      />
      <defs>
        <linearGradient
          id="paint0_linear_battle_solid_active"
          x1="22"
          y1="12"
          x2="2"
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
