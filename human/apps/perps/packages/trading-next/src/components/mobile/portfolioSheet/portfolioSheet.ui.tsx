import { FC, useCallback, useId } from "react";
import React from "react";
import { Trans, useTranslation } from "@paxeer-network/i18n";
import {
  ArrowDownShortIcon,
  ArrowLeftRightIcon,
  ArrowUpShortIcon,
  Button,
  cn,
  Divider,
  EyeCloseIcon,
  EyeIcon,
  Flex,
  Grid,
  Input,
  inputFormatter,
  InputFormatter,
  modal,
  PlusIcon,
  ReduceIcon,
  RefreshIcon,
  Statistic,
  Text,
} from "@paxeer-network/ui";
import { LeverageProps, LeverageSlider } from "@paxeer-network/ui-leverage";
import { USDCIcon } from "../accountSheet/icons";
import {
  getMarginRatioColor,
  PortfolioSheetState,
} from "./portfolioSheet.script";
import { RiskIndicator } from "./riskIndicator";

export const PortfolioSheet: FC<PortfolioSheetState> = (props) => {
  return (
    <Flex
      className="oui-portfolioSheet"
      direction={"column"}
      gap={4}
      width={"100%"}
    >
      <Asset {...props} />
      <Divider className="oui-w-full" />
      <MarginRatio {...props} />
      {/* <Leverage {...props} /> */}
      {/* <Divider className="oui-w-full" /> */}
      {/* <AvailableBalance {...props} /> */}
      <Buttons {...props} />
    </Flex>
  );
};

const Asset: FC<PortfolioSheetState> = (props) => {
  const { t } = useTranslation();

  const onUnsettleClick = useCallback(() => {
    return modal.confirm({
      title: t("settle.settlePnl"),
      // maxWidth: "xs",
      content: (
        <Text intensity={54} size="xs">
          {/* @ts-ignore */}
          <Trans i18nKey="settle.settlePnl.description" />
        </Text>
      ),
      onCancel: () => {
        return Promise.reject();
      },
      onOk: () => {
        if (typeof props.onSettlePnL !== "function") {
          return Promise.resolve();
        }
        return props.onSettlePnL().catch((e) => {});
      },
    });
  }, [t]);

  const clsName =
    props.totalUnrealizedROI > 0
      ? "oui-text-success-darken"
      : "oui-text-danger-darken";

  return (
    <Flex direction={"column"} gap={3} width={"100%"}>
      <Flex direction={"column"} itemAlign={"start"} width={"100%"}>
        <Text.formatted
          size="2xs"
          intensity={36}
          suffix={
            props.hideAssets ? (
              <EyeIcon
                opacity={1}
                size={16}
                className="oui-text-primary-light"
              />
            ) : (
              <EyeCloseIcon
                opacity={1}
                size={16}
                className="oui-text-primary-light"
              />
            )
          }
          onClick={() => {
            props.toggleHideAssets();
          }}
          className="oui-cursor-pointer"
        >
          {`${t("common.totalValue")} (USDC)`}
        </Text.formatted>
        <Text.numeral
          size="base"
          // coloring
          dp={2}
          padding={false}
          visible={!props.hideAssets}
        >
          {props.totalValue ?? "--"}
        </Text.numeral>
      </Flex>
      <Grid cols={2} rows={1} width={"100%"}>
        <Statistic
          label={`${t("common.unrealizedPnl")} (USDC)`}
          classNames={{
            label: "oui-text-2xs oui-text-base-contrast-36",
          }}
        >
          <Flex gap={1}>
            <Text.pnl
              size="xs"
              coloring
              dp={2}
              padding={false}
              visible={!props.hideAssets}
            >
              {props.aggregated.total_unreal_pnl}
            </Text.pnl>
            {!props.hideAssets && (
              <Text.roi
                size="xs"
                dp={2}
                padding={false}
                rule="percentages"
                prefix={"("}
                suffix={")"}
                className={clsName}
              >
                {props.totalUnrealizedROI}
              </Text.roi>
            )}
          </Flex>
        </Statistic>
        <Statistic
          label={`${t("trading.asset.unsettledPnl")} (USDC)`}
          classNames={{
            label: "oui-text-2xs oui-text-base-contrast-36",
          }}
        >
          <Flex justify={"between"} width={"100%"}>
            <Text.pnl
              size="xs"
              coloring
              dp={2}
              padding={false}
              visible={!props.hideAssets}
            >
              {props.aggregated.total_unsettled_pnl}
            </Text.pnl>
            <button
              className="oui-flex oui-gap-1 oui-items-center"
              onClick={onUnsettleClick}
            >
              <RefreshIcon
                opacity={1}
                size={12}
                className="oui-text-primary-light"
              />
              <Text size="2xs" color="primary">
                {t("settle.settlePnl")}
              </Text>
            </button>
          </Flex>
        </Statistic>
      </Grid>
    </Flex>
  );
};
const MarginRatio: FC<PortfolioSheetState> = (props) => {
  const { t } = useTranslation();

  const { high, mid, low } = getMarginRatioColor(
    props.marginRatioVal,
    props.mmr,
  );

  return (
    <Grid cols={2} rows={1} width={"100%"}>
      <Statistic
        label={t("trading.asset.marginRatio")}
        classNames={{
          label: "oui-text-2xs oui-text-base-contrast-36",
        }}
      >
        <Flex gap={2}>
          <Text.numeral
            size="xs"
            rule="percentages"
            color="primary"
            dp={2}
            padding={false}
            visible={!props.hideAssets}
          >
            {props.marginRatioVal}
          </Text.numeral>
          {!props.hideAssets && (
            <RiskIndicator
              className={
                low
                  ? "oui-rotate-0"
                  : mid
                    ? "oui-rotate-90"
                    : high
                      ? "oui-rotate-180"
                      : ""
              }
            />
          )}
        </Flex>
      </Statistic>
      <Statistic
        label={`${t("trading.asset.free&TotalCollateral")} (USDC)`}
        classNames={{
          label: "oui-text-2xs oui-text-base-contrast-36",
        }}
      >
        <Flex justify={"start"} width={"100%"} gap={1}>
          <Text.collateral
            size="xs"
            dp={2}
            padding={false}
            visible={!props.hideAssets}
          >
            {props.freeCollateral}
          </Text.collateral>
          <Text size="xs">/</Text>
          <Text.collateral
            size="xs"
            dp={2}
            padding={false}
            visible={!props.hideAssets}
          >
            {props.totalCollateral}
          </Text.collateral>
        </Flex>
      </Statistic>
    </Grid>
  );
};

const IconButton: React.FC<{
  Icon: React.ComponentType<any>;
  onClick: React.MouseEventHandler<SVGSVGElement>;
  disabled: boolean;
}> = (props) => {
  const { Icon, onClick, disabled } = props;
  return (
    <Icon
      onClick={disabled ? undefined : onClick}
      className={cn(
        "oui-text-base-contrast oui-m-2 oui-transition-all",
        disabled
          ? "oui-cursor-not-allowed oui-opacity-20"
          : "oui-cursor-pointer oui-opacity-100",
      )}
    />
  );
};

const LeverageInput: React.FC<PortfolioSheetState> = (props) => {
  const formatters = React.useMemo<InputFormatter[]>(
    () => [
      inputFormatter.numberFormatter,
      inputFormatter.currencyFormatter,
      inputFormatter.decimalPointFormatter,
    ],
    [],
  );
  const id = useId();
  return (
    <label
      htmlFor={id}
      className={cn(
        "oui-w-full",
        "oui-rounded",
        "oui-bg-base-6",
        "oui-flex",
        "oui-items-center",
        "oui-justify-between",
        "oui-outline",
        "oui-outline-offset-0",
        "oui-outline-1",
        "oui-outline-transparent",
        "focus-within:oui-outline-primary-light",
        "oui-input-root",
      )}
    >
      <IconButton
        Icon={ReduceIcon}
        onClick={props.onLeverageReduce}
        disabled={props.isReduceDisabled}
      />
      <Flex itemAlign="center" justify="center">
        <Input
          // {...props}
          value={props.value}
          id={id}
          autoComplete="off"
          classNames={{
            input: cn("oui-text-center"),
            root: cn(
              "oui-text-center",
              "oui-w-6",
              "oui-px-0",
              "oui-outline",
              "oui-outline-offset-0",
              "oui-outline-1",
              "oui-outline-transparent",
              "focus-within:oui-outline-primary-none",
            ),
          }}
          formatters={formatters}
          onChange={props.onInputChange}
        />
        <div className="oui-select-none">x</div>
      </Flex>
      <IconButton
        Icon={PlusIcon}
        onClick={props.onLeverageIncrease}
        disabled={props.isIncreaseDisabled}
      />
    </label>
  );
};

export const LeverageSelector: React.FC<PortfolioSheetState> = (props) => {
  const { value, onLeverageChange, onValueCommit } = props;
  return (
    <Flex itemAlign="center" justify="between" width={"100%"} mt={2}>
      {[1, 5, 10, 20, 50].map((option) => (
        <Flex
          key={option}
          itemAlign="center"
          justify="center"
          className={cn(
            `oui-transition-all oui-cursor-pointer oui-box-border oui-bg-clip-padding oui-px-3 oui-py-2.5 oui-rounded-md oui-border oui-border-solid`,
            value === option
              ? "oui-border-primary oui-bg-base-6"
              : "oui-border-line-12",
          )}
          onClick={() => {
            onLeverageChange(option);
            onValueCommit(option);
          }}
        >
          <Flex
            itemAlign="center"
            justify="center"
            className={cn(`oui-h-3 oui-w-9 oui-select-none`)}
          >
            {option}x
          </Flex>
        </Flex>
      ))}
    </Flex>
  );
};

const Buttons: FC<PortfolioSheetState> = (props) => {
  const { t } = useTranslation();

  if (props.isMainAccount) {
    return (
      <Grid
        cols={props.hasSubAccount ? 3 : 2}
        rows={1}
        gap={3}
        className="oui-grid-row-[1fr,1fr]"
        width={"100%"}
        pt={2}
        pb={4}
      >
        <Button
          icon={<ArrowDownShortIcon color="white" opacity={0.8} />}
          size="md"
          fullWidth
          onClick={props.onDeposit}
        >
          {t("common.deposit")}
        </Button>
        {props.hasSubAccount && (
          <Button
            icon={<ArrowLeftRightIcon color="white" opacity={0.8} />}
            color="gray"
            size="md"
            onClick={props.onTransfer}
            data-testid="oui-testid-assetView-transfer-button"
          >
            <Text>{t("common.transfer")}</Text>
          </Button>
        )}
        <Button
          icon={<ArrowUpShortIcon color="white" opacity={0.8} />}
          size="md"
          fullWidth
          className="oui-bg-base-2 hover:oui-bg-base-2/50"
          onClick={props.onWithdraw}
        >
          {t("common.withdraw")}
        </Button>
      </Grid>
    );
  }

  return (
    <Button
      fullWidth
      color="secondary"
      size="md"
      onClick={props.onTransfer}
      data-testid="oui-testid-assetView-transfer-button"
    >
      <Text>{t("common.transfer")}</Text>
    </Button>
  );
};
