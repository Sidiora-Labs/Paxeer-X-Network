import { BaseIcon, BaseIconProps } from "./baseIcon";

export const MarketsInactiveIcon = (props: BaseIconProps) => {
  return (
    <BaseIcon {...props}>
      <path
        className="oui-fill-base-contrast-12"
        fillRule="evenodd"
        clipRule="evenodd"
        d="M11.5 8a2 2 0 0 0-2 2v8a2 2 0 0 0 2 2h2a2 2 0 0 0 2-2v-8a2 2 0 0 0-2-2zm-7 4a2 2 0 0 0-2 2v4a2 2 0 0 0 2 2h2a2 2 0 0 0 2-2v-4a2 2 0 0 0-2-2z"
      />
      <path
        className="oui-fill-base-contrast-36"
        d="M16.5 6a2 2 0 0 1 2-2h2a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2h-2a2 2 0 0 1-2-2z"
      />
    </BaseIcon>
  );
};
