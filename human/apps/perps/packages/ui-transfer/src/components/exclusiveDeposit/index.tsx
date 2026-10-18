import { FC, useEffect, useState } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { Box, Flex, Text } from "@paxeer-network/ui";
import { CopyAddress } from "./CopyAddress";
import { DepositStatusBlock } from "./DepositStatus";
import { NetworkTokenSelect } from "./NetworkTokenSelect";
import { QRCodeDisplay } from "./QRCodeDisplay";
import { WarningBanner } from "./WarningBanner";
import { useExclusiveDeposit } from "./hooks/useExclusiveDeposit";
import { useExclusiveDepositOptions } from "./hooks/useExclusiveDepositOptions";

type ExclusiveDepositProps = {
  active?: boolean;
};

export const ExclusiveDeposit: FC<ExclusiveDepositProps> = ({ active }) => {
  const { t: t0 } = useTranslation();
  const t = t0 as any;
  const [selectedNetwork, setSelectedNetwork] = useState("");
  const [selectedToken, setSelectedToken] = useState("");

  const { networkOptions, tokenOptions, isSupported } =
    useExclusiveDepositOptions({
      selectedNetwork,
    });

  const confirmed = !!selectedNetwork && !!selectedToken;

  // Reset token when network changes and current token is unavailable
  useEffect(() => {
    if (
      selectedToken &&
      tokenOptions.length > 0 &&
      !tokenOptions.some((t) => t.value === selectedToken)
    ) {
      setSelectedToken("");
    }
  }, [selectedNetwork, selectedToken, tokenOptions]);

  const selectedNetworkOption = selectedNetwork
    ? networkOptions.find((n) => n.value === selectedNetwork)
    : undefined;
  const selectedChainId = selectedNetworkOption?.chainId;
  const selectedTokenOption = selectedToken
    ? tokenOptions.find((t) => t.value === selectedToken)
    : undefined;

  const {
    address,
    qrUri,
    minimumDeposit,
    estimatedArrivalText,
    latestEvent,
    pendingCount,
    explorerUrl,
  } = useExclusiveDeposit({
    active,
    confirmed,
    chainId: selectedChainId,
    token: selectedToken,
    explorerBaseUrl: selectedNetworkOption?.explorerUrl ?? "",
  });

  const networkName = selectedNetworkOption?.label ?? "";
  const tokenName = selectedTokenOption?.label ?? "";

  const warningMessage = confirmed
    ? t("transfer.exclusiveDeposit.warning", {
        token: tokenName,
        network: networkName,
      })
    : t("transfer.exclusiveDeposit.selectFirst");

  const minText =
    confirmed && typeof minimumDeposit === "number"
      ? `${minimumDeposit} ${tokenName}`
      : "--";

  const estText = confirmed
    ? (estimatedArrivalText ??
      t("transfer.exclusiveDeposit.estimatedTime.default"))
    : "--";

  return (
    <Box className="oui-flex oui-flex-col oui-items-center oui-rounded-xl oui-bg-base-8 oui-tracking-[0.03em]">
      <WarningBanner message={warningMessage} />

      {confirmed && (
        <>
          <QRCodeDisplay address={qrUri} />
          {address && <CopyAddress address={address} />}
        </>
      )}

      <Flex direction="column" gap={2} className="oui-mt-5 oui-w-full">
        <NetworkTokenSelect
          key={selectedNetwork}
          selectedNetwork={selectedNetwork}
          selectedToken={selectedToken}
          onNetworkChange={(value) => {
            setSelectedNetwork(value);
            setSelectedToken("");
          }}
          onTokenChange={setSelectedToken}
          networkOptions={networkOptions}
          tokenOptions={tokenOptions}
        />

        {/* Min. Deposit */}
        <Flex className="oui-w-full" justify="between">
          <Text size="xs" intensity={36}>
            {t("transfer.exclusiveDeposit.minDeposit")}
          </Text>
          <Text
            size="xs"
            className={confirmed ? "oui-text-warning-darken" : ""}
            intensity={confirmed ? undefined : 98}
          >
            {minText}
          </Text>
        </Flex>

        {/* Estimated time */}
        <Flex className="oui-w-full" justify="between">
          <Text size="xs" intensity={36}>
            {t("transfer.exclusiveDeposit.estimatedTime")}
          </Text>
          <Text size="xs" intensity={98}>
            {estText}
          </Text>
        </Flex>
      </Flex>

      {confirmed && latestEvent && explorerUrl && (
        <DepositStatusBlock
          amount={latestEvent.amount}
          symbol={latestEvent.token}
          pendingCount={pendingCount}
          explorerUrl={explorerUrl}
        />
      )}
    </Box>
  );
};
