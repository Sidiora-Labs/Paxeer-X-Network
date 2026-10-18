import { FC, useMemo, useState, useCallback, ReactNode } from "react";
import { useAccount, useLocalStorage } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { useAppContext } from "@paxeer-network/react-app";
import { AccountStatusEnum } from "@paxeer-network/types";
import {
  Flex,
  Text,
  Box,
  Button,
  ArrowDownShortIcon,
  EyeIcon,
  EyeCloseIcon,
  ChevronDownIcon,
  Tooltip,
  Divider,
  gradientTextVariants,
  cn,
} from "@paxeer-network/ui";
import { AuthGuard } from "@paxeer-network/ui-connector";
import { LTVRiskTooltipWidget } from "@paxeer-network/ui-order-entry";
import { AssetViewState } from "./assetView.script";
import { FaucetWidget } from "./faucet/faucet.widget";

interface StatusInfo {
  title: string;
  description: string;
  titleColor?: any;
  titleClsName?: string;
}

interface TooltipContentProps {
  description: ReactNode;
  formula: ReactNode;
}

interface TotalValueProps {
  totalValue?: number;
  visible?: boolean;
  onToggleVisibility?: () => void;
}

interface AssetDetailProps {
  label: string;
  description?: ReactNode;
  formula?: ReactNode;
  visible: boolean;
  value?: number | string;
  unit?: string;
  rule?: "percentages";
  isConnected?: boolean;
  showPercentage?: boolean;
  placeholder?: string;
}

interface AssetValueListProps {
  visible?: boolean;
  freeCollateral?: number | null;
  marginRatioVal?: number;
  renderMMR?: string | number;
  maintenanceMargin?: number;
  currentLeverage?: number;
  isConnected: boolean;
  currentLtv?: string | number;
}

const calculateTextColor = (val: number): string => {
  if (val >= 0 && val < 50) {
    return "oui-text-success";
  } else if (val >= 50 && val < 80) {
    return "oui-text-warning";
  } else if (val >= 80) {
    return "oui-text-danger";
  } else {
    return "";
  }
};

const useCurrentStatusText = (): StatusInfo => {
  const { state } = useAccount();
  const { wrongNetwork, disabledConnect } = useAppContext();
  const { t } = useTranslation();

  return useMemo(() => {
    const statusText = {
      wrongNetwork: {
        title: t("connector.wrongNetwork"),
        description: t("connector.wrongNetwork.tooltip"),
        titleColor: "warning",
      },
      connectWallet: {
        title: t("connector.connectWallet"),
        description: t("connector.trade.connectWallet.tooltip"),
        titleColor: "primary",
      },
      createAccount: {
        title: t("connector.createAccount"),
        description: t("connector.trade.createAccount.tooltip"),
        titleColor: "primary",
      },
      enableTrading: {
        title: t("connector.enableTrading"),
        description: t("connector.trade.enableTrading.tooltip"),
        titleColor: "primary",
      },
      empty: {
        title: "",
        description: "",
      },
    };

    if (disabledConnect) {
      return statusText.connectWallet;
    }

    if (wrongNetwork) {
      return statusText.wrongNetwork;
    }

    // EnableTradingWithoutConnected is enabled trading status, so we don't need to show anything
    if (state.status === AccountStatusEnum.EnableTradingWithoutConnected) {
      return statusText.empty;
    }

    if (state.status <= AccountStatusEnum.NotConnected) {
      return statusText.connectWallet;
    }

    if (state.status <= AccountStatusEnum.NotSignedIn) {
      return statusText.createAccount;
    }

    if (state.status < AccountStatusEnum.EnableTrading) {
      return statusText.enableTrading;
    }

    return statusText.empty;
  }, [t, state.status, wrongNetwork, disabledConnect]);
};

export const TooltipContent: FC<TooltipContentProps> = (props) => {
  const { description, formula } = props;
  return (
    <div className="oui-min-w-[204px] oui-max-w-[240px] oui-text-2xs oui-leading-normal oui-text-base-contrast-80">
      {typeof description !== "undefined" && description !== null && (
        <span>{description}</span>
      )}
      <Divider color="oui-border-line-10" my={2} />
      {typeof formula !== "undefined" && formula !== null && (
        <span>{formula}</span>
      )}
    </div>
  );
};

const TotalValue: FC<TotalValueProps> = (props) => {
  const { t } = useTranslation();
  const { totalValue, visible = true, onToggleVisibility } = props;
  return (
    <Flex
      direction="column"
      gap={1}
      className="oui-text-2xs"
      itemAlign="center"
    >
      <Text.numeral
        visible={visible}
        weight="bold"
        size="2xl"
        className={gradientTextVariants({ color: "brand" })}
        as="div"
        padding={false}
        dp={2}
      >
        {totalValue ?? "--"}
      </Text.numeral>
      <Flex gap={1} itemAlign="center">
        <Text size="2xs" color="neutral" weight="semibold">
          {`${t("trading.asset.myAssets")} (USDC)`}
        </Text>
        <button
          className="oui-assetView-toggle-visibility-btn"
          onClick={onToggleVisibility}
        >
          {visible ? (
            <EyeIcon size={18} className="oui-text-base-contrast-54" />
          ) : (
            <EyeCloseIcon size={18} className="oui-text-base-contrast-54" />
          )}
        </button>
      </Flex>
    </Flex>
  );
};

const AssetDetail: FC<AssetDetailProps> = (props) => {
  const {
    label,
    description,
    formula,
    visible,
    value,
    unit,
    rule,
    placeholder,
  } = props;
  return (
    <Flex justify="between">
      <Tooltip
        className={""}
        content={<TooltipContent description={description} formula={formula} />}
      >
        <Text
          size="2xs"
          color="neutral"
          weight="semibold"
          className="oui-cursor-pointer oui-border-b oui-border-dashed oui-border-line-12"
        >
          {label}
        </Text>
      </Tooltip>
      <Text.numeral
        visible={visible}
        size="2xs"
        unit={unit}
        unitClassName="oui-text-base-contrast-36 oui-ms-0.5"
        as="div"
        rule={rule}
        padding={false}
        dp={2}
        // suffix={value && unit}
        placeholder={placeholder}
      >
        {value || "--"}
      </Text.numeral>
    </Flex>
  );
};

const LTVDetail: FC<Pick<AssetDetailProps, "value" | "visible">> = (props) => {
  const { visible, value } = props;
  const { t } = useTranslation();
  return (
    <Flex justify="between">
      <Tooltip
        className={cn("oui-bg-base-6 oui-p-2")}
        content={<LTVRiskTooltipWidget />}
      >
        <Text
          size="2xs"
          color="neutral"
          weight="semibold"
          className="oui-cursor-pointer oui-border-b oui-border-dashed oui-border-line-12"
        >
          {t("transfer.LTV")}
        </Text>
      </Tooltip>
      <Text
        size="2xs"
        className={cn(
          "select-none",
          visible && calculateTextColor(Number(value)),
        )}
      >
        {visible ? `${value}%` : "*****"}
      </Text>
    </Flex>
  );
};

const AssetValueList: FC<AssetValueListProps> = (props) => {
  const {
    visible = true,
    freeCollateral,
    marginRatioVal,
    renderMMR,
    maintenanceMargin,
    currentLeverage,
    isConnected,
    currentLtv,
  } = props;

  const [optionsOpen, setOptionsOpen] = useLocalStorage(
    "paxeerNetwork_entry_asset_list_open",
    false,
  );
  const [open, setOpen] = useState<boolean>(optionsOpen);

  const { t } = useTranslation();

  const toggleOpen = useCallback(() => {
    setOpen((prevOpen) => !prevOpen);
    setTimeout(() => {
      setOptionsOpen(!open);
    }, 0);
  }, []);

  const showLTV =
    typeof currentLtv === "number" &&
    !Number.isNaN(currentLtv) &&
    currentLtv > 0;

  return (
    <Box className="oui-group">
      <Flex
        justify="center"
        gap={1}
        itemAlign="center"
        className="oui-cursor-pointer"
        onClick={toggleOpen}
      >
        <Divider className="oui-flex-1" />
        <ChevronDownIcon
          size={12}
          color="white"
          className={cn("oui-transition-transform", open && "oui-rotate-180")}
        />
        <Divider className="oui-flex-1" />
      </Flex>
      <Box
        style={{ transform: "translateZ(0)" }}
        className={cn(
          "oui-select-none oui-space-y-1.5 oui-overflow-hidden",
          "oui-transition-[max-height] oui-duration-150",
          "group-hover:oui-will-change-[max-height]",
          open
            ? showLTV
              ? "oui-max-h-[144px]"
              : "oui-max-h-[119px]"
            : "oui-max-h-0",
        )}
      >
        <AssetDetail
          label={t("trading.asset.freeCollateral")}
          description={t("trading.asset.freeCollateral.tooltip")}
          formula={t("trading.asset.freeCollateral.formula")}
          visible={visible}
          // TODO: change AssetDetail value
          value={freeCollateral! === 0 ? ("0" as any) : freeCollateral}
          unit="USDC"
        />
        <AssetDetail
          label={t("trading.asset.marginRatio")}
          description={t("trading.asset.marginRatio.tooltip")}
          formula={t("trading.asset.marginRatio.formula")}
          visible={visible}
          value={marginRatioVal}
          isConnected={isConnected}
          rule="percentages"
          showPercentage={true}
          placeholder="--%"
        />
        <AssetDetail
          label={t("trading.asset.maintenanceMarginRatio")}
          description={t("trading.asset.maintenanceMarginRatio.tooltip")}
          formula={t("trading.asset.maintenanceMarginRatio.formula")}
          visible={visible}
          value={renderMMR}
          rule="percentages"
          showPercentage={true}
          placeholder="--%"
        />
        <AssetDetail
          label={t("trading.asset.maintenanceMargin")}
          description={t("trading.asset.maintenanceMargin.tooltip")}
          formula={t("trading.asset.maintenanceMargin.formula")}
          visible={visible}
          value={maintenanceMargin}
          unit="USDC"
        />
        <AssetDetail
          label={t("trading.asset.currentLeverage")}
          description={t("trading.asset.currentLeverage.tooltip")}
          formula={t("trading.asset.currentLeverage.formula")}
          visible={visible}
          value={currentLeverage}
          unit="x"
        />
        {showLTV && <LTVDetail visible={visible} value={currentLtv} />}
      </Box>
    </Box>
  );
};

export const AssetView: FC<
  AssetViewState & { isFirstTimeDeposit?: boolean }
> = ({
  networkId,
  isFirstTimeDeposit,
  totalValue,
  onDeposit,
  onWithdraw,
  onTransfer,
  toggleVisible,
  visible,
  freeCollateral,
  marginRatioVal,
  renderMMR,
  maintenanceMargin,
  currentLeverage,
  isConnected,
  isMainAccount,
  hasSubAccount,
  currentLtv,
}) => {
  const { title, description, titleColor, titleClsName } =
    useCurrentStatusText();

  const { t } = useTranslation();

  const transferButton = hasSubAccount && (
    <Button
      className="oui-assetView-transfer-btn"
      fullWidth
      color="secondary"
      size="md"
      onClick={onTransfer}
      data-testid="oui-testid-assetView-transfer-button"
    >
      <Text>{t("common.transfer")}</Text>
    </Button>
  );

  const depositButton = isMainAccount && (
    <Button
      className="oui-assetView-deposit-btn"
      data-testid="oui-testid-assetView-deposit-button"
      fullWidth
      size="md"
      onClick={onDeposit}
    >
      {!hasSubAccount && (
        <ArrowDownShortIcon opacity={1} className="oui-text-primary-contrast" />
      )}
      <Text>{t("common.deposit")}</Text>
    </Button>
  );

  const withdrawButton = isMainAccount && (
    <Button
      className="oui-assetView-withdraw-btn"
      fullWidth
      color="secondary"
      size="md"
      onClick={onWithdraw}
      data-testid="oui-testid-assetView-withdraw-button"
    >
      {!hasSubAccount && (
        <ArrowDownShortIcon
          color="white"
          opacity={1}
          className="oui-rotate-180"
        />
      )}
      <Text>{t("common.withdraw")}</Text>
    </Button>
  );

  return (
    <Box className="oui-assetView oui-relative">
      {title && description && (
        <Flex direction="column" gap={1} className="oui-mb-[32px]">
          <Text
            size="lg"
            weight="bold"
            color={titleColor || "inherit"}
            className={titleClsName}
          >
            {title}
          </Text>
          <Text
            size="2xs"
            color="neutral"
            weight="semibold"
            className="oui-text-center"
          >
            {description}
          </Text>
        </Flex>
      )}
      <AuthGuard
        networkId={networkId}
        buttonProps={{ size: "md", fullWidth: true }}
      >
        {isFirstTimeDeposit && isMainAccount ? (
          <>
            <Box>
              <Flex direction="column" gap={1} className="oui-mb-[32px]">
                <Text.gradient size="lg" weight="bold" color="brand">
                  {t("trading.asset.startTrading")}
                </Text.gradient>
                <Text size="2xs" color="neutral" weight="semibold">
                  {t("trading.asset.startTrading.description")}
                </Text>
              </Flex>
            </Box>
            <Button
              className="oui-assetView-deposit-btn"
              data-testid="oui-testid-assetView-deposit-button"
              fullWidth
              size="md"
              onClick={onDeposit}
            >
              <ArrowDownShortIcon
                opacity={1}
                className="oui-text-primary-contrast"
              />
              <Text>{t("common.deposit")}</Text>
            </Button>

            <Box className="oui-mt-3">
              <FaucetWidget />
            </Box>
          </>
        ) : (
          <Box className="oui-space-y-4">
            <TotalValue
              totalValue={totalValue}
              visible={visible}
              onToggleVisibility={toggleVisible}
            />
            <AssetValueList
              visible={visible}
              freeCollateral={freeCollateral}
              marginRatioVal={marginRatioVal}
              renderMMR={renderMMR}
              maintenanceMargin={maintenanceMargin}
              currentLeverage={currentLeverage}
              isConnected={isConnected}
              currentLtv={currentLtv}
            />
            <Flex
              gap={isMainAccount ? (hasSubAccount ? 2 : 3) : 0}
              itemAlign="center"
            >
              {isMainAccount ? (
                <>
                  {depositButton}
                  {transferButton}
                  {withdrawButton}
                </>
              ) : (
                transferButton
              )}
            </Flex>
            {isMainAccount && <FaucetWidget />}
          </Box>
        )}
      </AuthGuard>
    </Box>
  );
};
