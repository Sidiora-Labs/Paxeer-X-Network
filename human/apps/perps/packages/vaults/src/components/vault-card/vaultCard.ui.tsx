import { FC, useMemo } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import {
  Text,
  cn,
  Button,
  ArrowRightUpSquareFillIcon,
  useScreen,
  Tooltip,
  InfoCircleIcon,
} from "@paxeer-network/ui";
import { AuthGuard } from "@paxeer-network/ui-connector";
import { Decimal } from "@paxeer-network/utils";
import { VaultStatus } from "../../types/vault";
import { parseMarkdownLinks } from "../../utils/parseMarkdownLinks";
import { VaultCardScript } from "./vaultCard.script";

export const formatAllTimeReturn = (
  status: VaultStatus,
  vaultAge: number | null,
  lifetimeApy: number,
): string => {
  if (status === "pre_launch" || (vaultAge !== null && vaultAge < 7)) {
    return "--";
  }
  if (lifetimeApy > 100) {
    return ">10000%";
  }
  return (
    new Decimal(lifetimeApy)
      .mul(100)
      .toDecimalPlaces(2, Decimal.ROUND_UP)
      .toFixed(2) + "%"
  );
};

export const VaultCard: FC<VaultCardScript> = (props) => {
  const {
    title,
    vaultInfo,
    lpInfo,
    description,
    isEVMConnected,
    isSOLConnected,
    openDepositAndWithdraw,
    availableBalance,
    openVaultWebsite,
    icon,
    isDepositDisabled,
    isWithdrawDisabled,
  } = props;

  const { t } = useTranslation();
  const { isMobile } = useScreen();

  const formattedAllTimeReturn = useMemo(
    () =>
      formatAllTimeReturn(
        vaultInfo.status,
        vaultInfo.vault_age,
        vaultInfo.lifetime_apy,
      ),
    [vaultInfo.status, vaultInfo.vault_age, vaultInfo.lifetime_apy],
  );

  // Get status tag config
  const getStatusTag = () => {
    const { status } = vaultInfo;

    if (status === "live") {
      return {
        text: t("vaults.card.status.active"),
        color: "rgb(var(--oui-color-success))",
        bgColor: "rgba(var(--oui-color-success) / 0.15)",
      };
    } else if (status === "pre_launch") {
      return {
        text: t("vaults.card.launchingSoon"),
        color: "rgb(var(--oui-color-warning))",
        bgColor: "rgba(var(--oui-color-warning) / 0.15)",
      };
    } else if (status === "closing") {
      return {
        text: t("vaults.card.status.closing"),
        color: "rgb(var(--oui-color-danger))",
        bgColor: "rgba(var(--oui-color-danger) / 0.15)",
      };
    } else if (status === "closed") {
      return {
        text: t("vaults.card.status.closed"),
        color: "rgb(var(--oui-color-base-foreground) / 0.54)",
        bgColor: "rgba(var(--oui-color-base-foreground) / 0.15)",
      };
    }
    return null;
  };

  const statusTag = getStatusTag();

  const supportVaultsList = useMemo(() => {
    const chains = Array.isArray(vaultInfo?.supported_chains)
      ? vaultInfo.supported_chains
      : [];
    return (
      <div className="oui-flex oui-items-center">
        {chains.map((chain, index) => (
          <img
            key={chain.chain_id}
            src={`/assets/static/network_logo/${chain.chain_id}.png`}
            alt={chain.chain_id}
            className={cn(
              "oui-relative",
              isMobile ? "oui-size-[18px]" : "oui-size-5",
            )}
            style={{
              marginLeft: index > 0 ? "-4px" : "0",
              zIndex: chains.length - index,
            }}
          />
        ))}
      </div>
    );
  }, [vaultInfo.supported_chains, isMobile]);

  return (
    <div className="oui-relative oui-h-[435px] oui-overflow-hidden oui-rounded-2xl oui-bg-base-9">
      {/* Background image
      <img
        src="/vaults/vaults-card-bg.png"
        alt=""
        style={{
          position: "absolute",
          top: 0,
          right: 0,
          width: "100%",
          height: "180px",
          objectFit: "cover",
          objectPosition: "top",
          zIndex: 0,
        }}
        onError={(e) => {
          e.currentTarget.style.display = "none";
        }}
      />
      */}

      <div
        className={cn(
          "oui-absolute oui-start-0 oui-top-0 oui-z-20 oui-size-full oui-p-6",
          "oui-overflow-y-auto oui-custom-scrollbar",
        )}
      >
        <div className="oui-flex oui-flex-col oui-gap-2">
          {/* Title + Status tag group, 4px spacing */}
          <div className="oui-flex oui-flex-col oui-gap-1">
            {/* Vault name */}
            <div className="oui-text-[18px] oui-font-semibold oui-text-base-contrast oui-break-words">
              {title}
            </div>
            {/* Status tag */}
            {statusTag && (
              <div
                className="oui-flex oui-w-fit oui-items-center oui-gap-[10px] oui-px-2"
                style={{
                  height: "18px",
                  borderRadius: "4px",
                  background: statusTag.bgColor,
                  color: statusTag.color,
                  fontSize: "12px",
                  fontWeight: 600,
                  lineHeight: "18px",
                  letterSpacing: "0.36px",
                }}
              >
                {statusTag.text}
              </div>
            )}
          </div>

          {/* Description + View more block (gap-1 = 4px) */}
          <div className="oui-flex oui-flex-col oui-gap-1">
            <div className="oui-h-[54px]">
              <div className="oui-text-2xs oui-font-normal oui-leading-[18px] oui-text-base-contrast-54 oui-line-clamp-3">
                {parseMarkdownLinks(description)}
              </div>
            </div>
            {/* View more link */}
            <button
              onClick={openVaultWebsite}
              className="oui-flex oui-w-fit oui-items-center oui-gap-1 oui-text-xs oui-font-medium oui-text-primary"
            >
              <span>{t("vaults.card.viewMore")}</span>
              <ArrowRightUpSquareFillIcon
                className="oui-text-primary"
                width={16}
                height={16}
                viewBox="0 0 18 18"
              />
            </button>
          </div>

          {/* KPI + LP block (gap-6 = 24px) */}
          <div className="oui-flex oui-flex-col oui-gap-6">
            <div className="oui-flex oui-items-center oui-gap-2">
              <VaultInfoItem
                label={t("vaults.card.tvl")}
                value={vaultInfo.tvl}
                textProps={{
                  currency: "$",
                  dp: 0,
                  type: "numeral",
                }}
              />
              <VaultInfoItem
                label={t("vaults.card.allTimeReturn")}
                value={formattedAllTimeReturn}
                textProps={{
                  color: "brand",
                  type: "gradient",
                }}
                showTooltip={
                  vaultInfo.status === "pre_launch" ||
                  (vaultInfo.vault_age !== null && vaultInfo.vault_age < 7)
                }
                tooltipContent={t("vaults.card.allTimeReturnTooltip")}
              />
            </div>

            <div className="oui-flex oui-flex-col oui-items-center oui-gap-2 oui-rounded-lg oui-bg-base-contrast-6 oui-p-3">
              <LpInfoItem
                label={t("vaults.card.myDeposits")}
                value={lpInfo.deposits}
              />
              <LpInfoItem
                label={t("vaults.card.myEarnings")}
                value={lpInfo.earnings}
              />
            </div>
          </div>

          {/* Account balance + Buttons block (gap-4 = 16px, mt-2 for total 16px from My Deposits) */}
          <div className="oui-flex oui-flex-col oui-gap-4 oui-mt-2">
            <LpInfoItem
              label={t("vaults.card.accountBalance")}
              value={availableBalance}
            />

            <VaultCardOperation
              isEVMConnected={isEVMConnected}
              isSOLConnected={isSOLConnected}
              openDepositAndWithdraw={openDepositAndWithdraw}
              isDepositDisabled={isDepositDisabled}
              isWithdrawDisabled={isWithdrawDisabled}
            />
          </div>
        </div>
      </div>
    </div>
  );
};

const VaultInfoItem: FC<{
  label: string;
  value: string | number;
  textProps?: any;
  showTooltip?: boolean;
  tooltipContent?: string;
}> = (props) => {
  const { label, value, textProps, showTooltip, tooltipContent } = props;

  return (
    <div
      className={cn(
        "oui-flex oui-flex-1 oui-flex-col oui-items-center oui-justify-center oui-px-3 oui-py-2",
        "oui-rounded-lg oui-border oui-border-solid oui-border-line-12",
      )}
    >
      <div className="oui-flex oui-items-center oui-gap-1 oui-text-2xs oui-font-normal oui-leading-[18px] oui-text-base-contrast-54">
        {label}
        {showTooltip && tooltipContent && (
          <Tooltip content={tooltipContent} delayDuration={100}>
            <div>
              <InfoCircleIcon
                size={12}
                opacity={0.54}
                className="oui-text-base-contrast"
              />
            </div>
          </Tooltip>
        )}
      </div>
      {textProps.type === "gradient" ? (
        <Text.gradient
          className="oui-text-base oui-font-semibold"
          {...textProps}
        >
          {value}
        </Text.gradient>
      ) : (
        <Text.numeral
          className="oui-text-base oui-font-semibold"
          {...textProps}
        >
          {value}
        </Text.numeral>
      )}
    </div>
  );
};

const LpInfoItem: FC<{
  label: string;
  value: string | number;
  textProps?: any;
}> = (props) => {
  const { label, value, textProps } = props;

  return (
    <div className="oui-flex oui-w-full oui-items-center oui-justify-between oui-text-2xs oui-font-normal">
      <div className="oui-text-base-contrast-54">{label}</div>
      <Text.numeral
        className="oui-text-base-contrast-80"
        {...textProps}
        dp={2}
        suffix={
          <span className="oui-ms-1 oui-text-base-contrast-36">USDC</span>
        }
      >
        {value}
      </Text.numeral>
    </div>
  );
};

type VaultCardOperationProps = {
  isEVMConnected: boolean;
  isSOLConnected: boolean;
  openDepositAndWithdraw: (activeTab: "deposit" | "withdraw") => void;
  isDepositDisabled: boolean;
  isWithdrawDisabled: boolean;
};

const VaultCardOperation: FC<VaultCardOperationProps> = (props) => {
  const {
    isEVMConnected,
    isSOLConnected,
    openDepositAndWithdraw,
    isDepositDisabled,
    isWithdrawDisabled,
  } = props;
  const { t } = useTranslation();

  return (
    <AuthGuard buttonProps={{ size: "md", fullWidth: true }}>
      {isEVMConnected || isSOLConnected ? (
        <div className="oui-flex oui-items-center oui-gap-2">
          <Button
            className="oui-flex-1"
            size="md"
            disabled={isDepositDisabled}
            onClick={() => openDepositAndWithdraw("deposit")}
          >
            {t("common.deposit")}
          </Button>
          <Button
            className="oui-flex-1"
            size="md"
            color="secondary"
            disabled={isWithdrawDisabled}
            onClick={() => openDepositAndWithdraw("withdraw")}
          >
            {t("common.withdraw")}
          </Button>
        </div>
      ) : (
        <Button size="md" color="warning">
          {t("connector.wrongNetwork")}
        </Button>
      )}
    </AuthGuard>
  );
};
