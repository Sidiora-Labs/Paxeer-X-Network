import React from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { API } from "@paxeer-network/types";
import {
  Badge,
  Box,
  cn,
  Flex,
  Spinner,
  Text,
  TokenIcon,
  useDocumentDirection,
} from "@paxeer-network/ui";
import { Decimal } from "@paxeer-network/utils";
import { isYieldBearingAsset } from "../../constants/yieldBearingAssets";
import { useYieldAPY } from "../depositForm/hooks/useYieldAPY";

interface TokenOptionProps {
  token: API.TokenInfo & {
    label: string;
    value: string;
    insufficientBalance?: boolean;
    balance?: string;
    quantity?: string;
    amount?: number;
  };
  onTokenChange?: (token: API.TokenInfo) => void;
  isActive: boolean;
  index?: number;
  displayType?: "balance" | "vaultBalance";
  open?: boolean;
  isLoading?: boolean;
  showBalance?: boolean;
}

export const TokenOption: React.FC<TokenOptionProps> = (props) => {
  const {
    token,
    isActive,
    displayType,
    onTokenChange,
    open,
    isLoading,
    showBalance,
  } = props;
  const { symbol, precision, insufficientBalance } = token;
  const { apy } = useYieldAPY(symbol);
  const dir = useDocumentDirection();
  const isRTL = dir === "rtl";

  const showAPY = isYieldBearingAsset(symbol) && apy !== null;

  const { t } = useTranslation();

  if (displayType === "vaultBalance" && insufficientBalance) {
    return (
      <Flex
        key={symbol}
        direction={isRTL ? "rowReverse" : "row"}
        itemAlign={"center"}
        justify="between"
        px={2}
        r="base"
        className={cn(
          "group",
          "oui-h-[30px]",
          "oui-text-2xs oui-font-semibold",
          isActive && "oui-bg-base-5",
          props.index !== 0 && "oui-mt-[2px]",
          "oui-cursor-not-allowed",
        )}
      >
        <Flex
          direction={isRTL ? "rowReverse" : "row"}
          itemAlign="center"
          gapX={1}
        >
          <TokenIcon
            url={(token as any).logo_uri}
            name={symbol}
            className="oui-size-[16px] oui-opacity-50"
          />
          <Text intensity={36} className="oui-max-w-[200px] oui-truncate">
            {token.label}
          </Text>
          <Badge color="neutral" size="xs">
            {t("transfer.withdraw.InsufficientVaultBalance")}
          </Badge>
        </Flex>
      </Flex>
    );
  }

  const renderValue = () => {
    if (!showBalance) {
      return null;
    }

    if (isLoading && !token.amount) {
      return <Spinner size="sm" />;
    }

    return (
      <Text.numeral
        rule="price"
        dp={precision}
        rm={Decimal.ROUND_DOWN}
        ignoreDP
        className={cn(
          "oui-text-base-contrast-80 group-hover:oui-text-base-contrast-54",
          isActive && "oui-text-base-contrast-54",
        )}
      >
        {token.amount!}
      </Text.numeral>
    );
  };

  return (
    <Flex
      key={symbol}
      direction={isRTL ? "rowReverse" : "row"}
      justify="between"
      px={2}
      r="base"
      className={cn(
        "group",
        "oui-h-[30px] hover:oui-bg-base-5",
        "oui-text-2xs oui-font-semibold",
        "oui-cursor-pointer",
        isActive && "oui-bg-base-5",
        props.index !== 0 && "oui-mt-[2px]",
      )}
      onClick={() => {
        onTokenChange?.(token);
      }}
    >
      <Flex
        direction={isRTL ? "rowReverse" : "row"}
        gapX={1}
        itemAlign="center"
      >
        <TokenIcon
          name={symbol}
          url={(token as any).logo_uri}
          className="oui-size-[16px]"
        />
        <Text
          className={cn(
            "oui-text-base-contrast-54 group-hover:oui-text-base-contrast-80",
            isActive && "oui-text-base-contrast-80",
            "oui-max-w-[200px] oui-truncate",
          )}
        >
          {token.label}
        </Text>
        {showAPY && (
          <Box
            height={18}
            px={2}
            r="sm"
            className="oui-rounded-[4px] oui-bg-success/[0.15]"
          >
            <Text size="2xs" className="oui-text-success">
              {apy!.toFixed(1)}% APY
            </Text>
          </Box>
        )}
      </Flex>
      {renderValue()}
    </Flex>
  );
};
