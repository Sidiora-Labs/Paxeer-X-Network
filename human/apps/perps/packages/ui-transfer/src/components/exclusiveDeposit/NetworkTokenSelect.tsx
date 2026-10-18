import { FC } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import {
  Box,
  ChainIcon,
  Flex,
  Select,
  SelectItem,
  Text,
  TokenIcon,
} from "@paxeer-network/ui";
import {
  type ExclusiveDepositNetwork,
  type ExclusiveDepositToken,
} from "./hooks/useExclusiveDepositOptions";

type NetworkTokenSelectProps = {
  selectedNetwork: string;
  selectedToken: string;
  onNetworkChange: (value: string) => void;
  onTokenChange: (value: string) => void;
  networkOptions: ExclusiveDepositNetwork[];
  tokenOptions: ExclusiveDepositToken[];
};

export const NetworkTokenSelect: FC<NetworkTokenSelectProps> = ({
  selectedNetwork,
  selectedToken,
  onNetworkChange,
  onTokenChange,
  networkOptions,
  tokenOptions,
}) => {
  const { t: t0 } = useTranslation();
  const t = t0 as any;

  const selectContentProps = {
    align: "end" as const,
    sideOffset: -4,
    className: "oui-custom-scrollbar",
  };

  return (
    <>
      {/* Network */}
      <Flex className="oui-w-full" justify="between" itemAlign="center">
        <Text size="xs" intensity={36}>
          {t("common.network")}
        </Text>
        <Box className="oui-ms-auto">
          <Select
            size="xs"
            value={selectedNetwork || undefined}
            onValueChange={onNetworkChange}
            placeholder={t("transfer.exclusiveDeposit.selectNetwork")}
            variant="text"
            classNames={{ trigger: "oui-pe-0" }}
            maxHeight={128}
            contentProps={selectContentProps}
          >
            {networkOptions.map((opt) => (
              <SelectItem key={opt.value} value={opt.value}>
                <Flex gap={1} itemAlign="center">
                  <ChainIcon chainId={opt.chainId} size="2xs" />
                  {opt.label}
                </Flex>
              </SelectItem>
            ))}
          </Select>
        </Box>
      </Flex>

      {/* Token */}
      <Flex className="oui-w-full" justify="between" itemAlign="center">
        <Text size="xs" intensity={36}>
          {t("common.token")}
        </Text>
        <Box className="oui-ms-auto">
          <Select
            size="xs"
            value={selectedToken || undefined}
            onValueChange={onTokenChange}
            placeholder={t("transfer.exclusiveDeposit.selectToken")}
            variant="text"
            classNames={{ trigger: "oui-pe-0" }}
            maxHeight={128}
            contentProps={selectContentProps}
          >
            {tokenOptions.map((opt) => (
              <SelectItem key={opt.value} value={opt.value}>
                <Flex gap={1} itemAlign="center">
                  <TokenIcon name={opt.value} size="2xs" />
                  {opt.label}
                </Flex>
              </SelectItem>
            ))}
          </Select>
        </Box>
      </Flex>
    </>
  );
};
