import { FC, useState } from "react";
import { Box, Flex, Text } from "@paxeer-network/ui";

const CopyIcon: FC<{ copied: boolean }> = ({ copied }) => (
  <svg
    width="17"
    height="16"
    viewBox="0 0 17 16"
    fill="currentColor"
    xmlns="http://www.w3.org/2000/svg"
    className={`oui-fill-base-contrast oui-text-base-contrast-54 ${copied ? "oui-fill-success oui-text-success" : ""}`}
  >
    {copied ? (
      <path
        d="M14 4L6 11.5L3 8.5"
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    ) : (
      <path
        d="M5.166 1.994A2.667 2.667 0 0 0 2.499 4.66v4a2.667 2.667 0 0 0 2.667 2.667 2.667 2.667 0 0 0 2.666 2.667h4a2.667 2.667 0 0 0 2.667-2.667v-4a2.667 2.667 0 0 0-2.667-2.667 2.667 2.667 0 0 0-2.666-2.666zm6.666 4c.737 0 1.334.596 1.334 1.333v4c0 .737-.597 1.334-1.334 1.334h-4A1.333 1.333 0 0 1 6.5 11.327h2.667a2.667 2.667 0 0 0 2.666-2.667z"
        fill="currentColor"
      />
    )}
  </svg>
);

type CopyAddressProps = {
  address: string;
};

export const CopyAddress: FC<CopyAddressProps> = ({ address }) => {
  const [copied, setCopied] = useState(false);

  const handleCopy = () => {
    navigator.clipboard.writeText(address);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <Flex
      gap={1}
      itemAlign="start"
      className="oui-mt-6 oui-w-full oui-cursor-pointer oui-justify-center"
      onClick={handleCopy}
    >
      <Text
        size="xs"
        className="oui-break-all oui-text-center oui-text-base-contrast-80"
        title={address}
      >
        {address}
      </Text>
      <Box className="oui-mt-0.5 oui-shrink-0">
        <CopyIcon copied={copied} />
      </Box>
    </Flex>
  );
};
