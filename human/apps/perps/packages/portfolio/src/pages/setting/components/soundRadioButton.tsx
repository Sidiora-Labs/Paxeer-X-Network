import type { FC } from "react";
import { Flex, Text } from "@paxeer-network/ui";

export type SoundRadioButtonProps = {
  sel: boolean;
  label: string;
  onCheckChange: () => void;
};

export const SoundRadioButton: FC<SoundRadioButtonProps> = ({
  sel,
  label,
  onCheckChange,
}) => {
  return (
    <Flex
      onClick={(e) => {
        onCheckChange();
        e.stopPropagation();
      }}
      gap={1}
      className="oui-cursor-pointer"
    >
      {sel ? (
        <SoundSelIcon className="oui-fill-base-contrast" />
      ) : (
        <SoundUnselIcon className="oui-fill-base-contrast-54" />
      )}
      <Text size="2xs" intensity={sel ? 98 : 54}>
        {label}
      </Text>
    </Flex>
  );
};

export const SoundSelIcon = (props: { className: string }) => {
  return (
    <svg
      width="16"
      height="16"
      viewBox="0 0 16 16"
      fill="currentColor"
      xmlns="http://www.w3.org/2000/svg"
      className={props.className}
    >
      <path d="M8.01 1.333a6.667 6.667 0 1 0 0 13.333 6.667 6.667 0 0 0 0-13.333m0 1.333a5.334 5.334 0 1 1-.001 10.667 5.334 5.334 0 0 1 0-10.667" />
      <circle cx="8" cy="8" r="3.333" />
    </svg>
  );
};

export const SoundUnselIcon = (props: { className: string }) => {
  return (
    <svg
      width="16"
      height="16"
      viewBox="0 0 16 16"
      fill="currentColor"
      xmlns="http://www.w3.org/2000/svg"
      className={props.className}
    >
      <path d="M8.01 1.333a6.667 6.667 0 1 0 0 13.333 6.667 6.667 0 0 0 0-13.333m0 1.333a5.334 5.334 0 1 1-.001 10.667 5.334 5.334 0 0 1 0-10.667" />
    </svg>
  );
};
