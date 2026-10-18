import { FC, useMemo, useState } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import {
  cn,
  Text,
  Button,
  ArrowRightUpSquareFillIcon,
  useScreen,
  Tooltip,
  InfoCircleIcon,
} from "@paxeer-network/ui";
import { AuthGuard } from "@paxeer-network/ui-connector";
import { VaultInfo } from "../../types/vault";
import { useVaultCardScript } from "../vault-card/vaultCard.script";
import { formatAllTimeReturn } from "../vault-card/vaultCard.ui";

type SortField = "tvl" | "apy" | "deposits" | "pnl" | "balance";
type SortDirection = "asc" | "desc";

interface VaultsListProps {
  vaults: VaultInfo[];
}

export const VaultsList: FC<VaultsListProps> = ({ vaults }) => {
  const { t } = useTranslation();
  const [sortField, setSortField] = useState<SortField>("apy");
  const [sortDirection, setSortDirection] = useState<SortDirection>("desc");

  const shouldShowApyTooltip = useMemo(() => {
    return vaults.some(
      (vault) =>
        vault.status === "pre_launch" ||
        (vault.vault_age !== null && vault.vault_age < 7),
    );
  }, [vaults]);

  const handleSort = (field: SortField) => {
    if (sortField === field) {
      setSortDirection(sortDirection === "asc" ? "desc" : "asc");
    } else {
      setSortField(field);
      setSortDirection("desc");
    }
  };

  const sortedVaults = useMemo(() => {
    return [...vaults].sort((a, b) => {
      let aValue = 0;
      let bValue = 0;

      switch (sortField) {
        case "tvl":
          aValue = a.tvl;
          bValue = b.tvl;
          break;
        case "apy":
          aValue = a.lifetime_apy;
          bValue = b.lifetime_apy;
          break;
        // For deposits, pnl, balance - these need LP info which we'll handle in VaultListRow
        default:
          return 0;
      }

      return sortDirection === "asc" ? aValue - bValue : bValue - aValue;
    });
  }, [vaults, sortField, sortDirection]);

  const SortIcon: FC<{ field: SortField }> = ({ field }) => {
    const isActive = sortField === field;
    return (
      <svg
        width="12"
        height="12"
        viewBox="0 0 12 12"
        fill="none"
        xmlns="http://www.w3.org/2000/svg"
        className={cn(
          "oui-ms-1 oui-transition-colors",
          isActive ? "oui-text-base-contrast" : "oui-text-base-contrast-36",
        )}
      >
        <path
          d="M6 2L8 4H4L6 2Z"
          fill="currentColor"
          opacity={isActive && sortDirection === "asc" ? 1 : 0.3}
        />
        <path
          d="M6 10L4 8H8L6 10Z"
          fill="currentColor"
          opacity={isActive && sortDirection === "desc" ? 1 : 0.3}
        />
      </svg>
    );
  };

  return (
    <div className="oui-flex oui-flex-col oui-gap-3">
      {/* Table Header */}
      <div className="oui-grid oui-grid-cols-[2fr_1fr_1fr_1fr_1fr_1fr_1.5fr] oui-gap-4 oui-px-4 oui-py-3">
        <div className="oui-text-2xs oui-font-normal oui-text-base-contrast-54">
          {t("vaults.list.poolName")}
        </div>
        <button
          onClick={() => handleSort("tvl")}
          className="oui-flex oui-items-center oui-text-2xs oui-font-normal oui-text-base-contrast-54 hover:oui-text-base-contrast"
        >
          TVL
          <SortIcon field="tvl" />
        </button>
        <div className="oui-flex oui-items-center oui-gap-1">
          <button
            onClick={() => handleSort("apy")}
            className="oui-flex oui-items-center oui-text-2xs oui-font-normal oui-text-base-contrast-54 hover:oui-text-base-contrast"
          >
            {t("vaults.card.allTimeReturn")}
            <SortIcon field="apy" />
          </button>
          {shouldShowApyTooltip && (
            <Tooltip
              content={t("vaults.card.allTimeReturnTooltip")}
              delayDuration={100}
            >
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
        <button
          onClick={() => handleSort("deposits")}
          className="oui-flex oui-items-center oui-text-2xs oui-font-normal oui-text-base-contrast-54 hover:oui-text-base-contrast"
        >
          {t("vaults.list.myDeposits")}
          <SortIcon field="deposits" />
        </button>
        <button
          onClick={() => handleSort("pnl")}
          className="oui-flex oui-items-center oui-text-2xs oui-font-normal oui-text-base-contrast-54 hover:oui-text-base-contrast"
        >
          {t("vaults.list.allTimePnl")}
          <SortIcon field="pnl" />
        </button>
        <div className="oui-text-2xs oui-font-normal oui-text-base-contrast-54">
          {t("vaults.card.accountBalance")}
        </div>
        <div className="oui-text-2xs oui-font-normal oui-text-base-contrast-54">
          {t("vaults.list.operate")}
        </div>
      </div>

      {/* Table Rows */}
      <div className="oui-flex oui-flex-col oui-gap-2">
        {sortedVaults.map((vault) => (
          <VaultListRow key={vault.vault_id} vault={vault} />
        ))}
      </div>
    </div>
  );
};

const VaultListRow: FC<{ vault: VaultInfo }> = ({ vault }) => {
  const { t } = useTranslation();
  const script = useVaultCardScript(vault);

  const {
    title,
    vaultInfo,
    lpInfo,
    isEVMConnected,
    isSOLConnected,
    openDepositAndWithdraw,
    availableBalance,
    openVaultWebsite,
    icon,
    isDepositDisabled,
    isWithdrawDisabled,
  } = script;

  const isPreLaunch = vaultInfo.status === "pre_launch";
  const { isMobile } = useScreen();

  // Get status tag config (same as card mode)
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

  const formattedAllTimeReturn = useMemo(
    () =>
      formatAllTimeReturn(
        vaultInfo.status,
        vaultInfo.vault_age,
        vaultInfo.lifetime_apy,
      ),
    [vaultInfo.status, vaultInfo.vault_age, vaultInfo.lifetime_apy],
  );

  const supportVaultsList = useMemo(() => {
    const chains = Array.isArray(vaultInfo?.supported_chains)
      ? vaultInfo.supported_chains
      : [];

    const ICON_SIZE = isMobile ? 18 : 20;

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
              width: ICON_SIZE,
              height: ICON_SIZE,
            }}
          />
        ))}
      </div>
    );
  }, [vaultInfo.supported_chains, isMobile]);

  return (
    <div className="oui-relative oui-grid oui-grid-cols-[2fr_1fr_1fr_1fr_1fr_1fr_1.5fr] oui-gap-4 oui-rounded-2xl oui-bg-base-9 oui-px-4 oui-py-4 oui-items-center oui-overflow-hidden">
      {/* Background image
      <img
        src="/vaults/vaults-list-bg.png"
        alt=""
        style={{
          position: "absolute",
          top: 0,
          left: 0,
          width: "40%",
          height: "100%",
          objectFit: "cover",
          objectPosition: "left",
          zIndex: 0,
        }}
        onError={(e) => {
          e.currentTarget.style.display = "none";
        }}
      />
      */}
      {/* Pool Name */}
      <div className="oui-relative oui-z-10 oui-flex oui-items-center oui-gap-2">
        {/* <img
          src={icon}
          alt={vaultInfo.broker_id}
          className="oui-size-10 oui-rounded-full"
          onError={(e) => {
            e.currentTarget.style.display = "none";
          }}
        /> */}
        <div className="oui-flex oui-flex-col oui-gap-1">
          <div className="oui-text-sm oui-font-semibold oui-text-base-contrast">
            {title}
          </div>
          <div className="oui-flex oui-flex-wrap oui-items-center oui-gap-2">
            {/* <div className="oui-flex oui-items-center oui-gap-2 oui-flex-shrink-0">
              {supportVaultsList}
            </div> */}
            {statusTag && (
              <div
                className="oui-flex oui-items-center oui-gap-[10px] oui-px-2 oui-whitespace-nowrap oui-flex-shrink-0"
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
            <button
              onClick={openVaultWebsite}
              className="oui-flex oui-items-center oui-gap-1 oui-text-xs oui-font-medium oui-text-primary"
            >
              <span>{t("vaults.card.viewMore")}</span>
              <ArrowRightUpSquareFillIcon
                className="oui-fill-primary"
                width={16}
                height={16}
                viewBox="0 0 18 18"
              />
            </button>
          </div>
        </div>
      </div>

      {/* TVL */}
      <div className="oui-relative oui-z-10">
        <Text.numeral
          className="oui-text-sm oui-font-normal oui-text-base-contrast"
          currency="$"
          dp={0}
        >
          {vaultInfo.tvl}
        </Text.numeral>
      </div>

      {/* All-time return */}
      <div className="oui-relative oui-z-10">
        <Text.gradient className="oui-text-sm oui-font-semibold" color="brand">
          {formattedAllTimeReturn}
        </Text.gradient>
      </div>

      {/* My Deposits */}
      <div className="oui-relative oui-z-10">
        <Text.numeral
          className="oui-text-sm oui-font-normal oui-text-base-contrast"
          dp={2}
        >
          {lpInfo.deposits}
        </Text.numeral>
      </div>

      {/* All-time PnL */}
      <div className="oui-relative oui-z-10">
        <Text.numeral
          className="oui-text-sm oui-font-normal oui-text-base-contrast"
          dp={2}
        >
          {lpInfo.earnings}
        </Text.numeral>
      </div>

      {/* Account Balance */}
      <div className="oui-relative oui-z-10">
        <Text.numeral
          className="oui-text-sm oui-font-normal oui-text-base-contrast"
          dp={2}
        >
          {availableBalance}
        </Text.numeral>
      </div>

      {/* Operate */}
      <div className="oui-relative oui-z-10 oui-flex oui-items-center oui-gap-2">
        <AuthGuard buttonProps={{ size: "sm" }}>
          {isEVMConnected || isSOLConnected ? (
            <>
              <Button
                size="sm"
                className="oui-flex-1"
                disabled={isDepositDisabled}
                onClick={() => openDepositAndWithdraw("deposit")}
              >
                {t("common.deposit")}
              </Button>
              <Button
                size="sm"
                color="secondary"
                className="oui-flex-1"
                disabled={isWithdrawDisabled}
                onClick={() => openDepositAndWithdraw("withdraw")}
              >
                {t("common.withdraw")}
              </Button>
            </>
          ) : (
            <Button size="sm" color="warning" fullWidth>
              {t("connector.wrongNetwork")}
            </Button>
          )}
        </AuthGuard>
      </div>
    </div>
  );
};
