import { FC } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { Flex, Text, ChevronRightIcon } from "@paxeer-network/ui";

type DepositStatusBlockProps = {
  amount: number;
  symbol?: string;
  pendingCount: number;
  explorerUrl: string;
};

export const DepositStatusBlock: FC<DepositStatusBlockProps> = ({
  amount,
  symbol = "USDC",
  pendingCount,
  explorerUrl,
}) => {
  const { t: t0 } = useTranslation();
  const t = t0 as any;
  const label = t("transfer.exclusiveDeposit.depositPending", {
    amount,
    symbol,
  });

  const handleClick = () => {
    window.open(explorerUrl, "_blank", "noopener,noreferrer");
  };

  return (
    <Flex
      itemAlign="center"
      gap={2}
      className="oui-group oui-mt-6 oui-w-full oui-cursor-pointer"
      onClick={handleClick}
    >
      <div className="oui-size-2 oui-shrink-0 oui-rounded-full oui-bg-primary" />
      <Text size="xs" intensity={98} className="oui-flex-1">
        {label}
      </Text>
      {pendingCount > 1 && (
        <Flex
          justify="center"
          itemAlign="center"
          r="full"
          width={18}
          height={18}
          className="oui-bg-line-12"
        >
          <Text size="3xs" intensity={80}>
            {pendingCount}
          </Text>
        </Flex>
      )}
      <ChevronRightIcon className="oui-size-4 oui-text-base-contrast-54 oui-transition-colors group-hover:oui-text-base-contrast-80" />
    </Flex>
  );
};
