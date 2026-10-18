import React, { useMemo } from "react";
import pick from "ramda/es/pick";
import { SubAccount } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { useAppContext } from "@paxeer-network/react-app";
import {
  Text,
  Card,
  Flex,
  gradientTextVariants,
  EyeIcon,
  EyeCloseIcon,
  cn,
  DataFilter,
  formatAddress,
  Tabs,
  TabPanel,
  ArrowDownShortIcon,
  Button,
  Divider,
} from "@paxeer-network/ui";
import { AuthGuardDataTable } from "@paxeer-network/ui-connector";
import type { SelectOption } from "@paxeer-network/ui/src/select/withOptions";
import type { useAssetsScriptReturn } from "./assets.script";
import type {
  AssetsDataTableWidgetProps,
  AssetsWidgetProps,
} from "./assets.widget";

const LazyConvertHistoryWidget = React.lazy(() =>
  import("../convertPage/convert.widget").then((mod) => {
    return { default: mod.ConvertHistoryWidget };
  }),
);

export type AssetsProps = useAssetsScriptReturn;

export enum AccountType {
  ALL = "All accounts",
  MAIN = "Main accounts",
}

const TotalValueInfo: React.FC<
  Readonly<
    Pick<AssetsWidgetProps, "totalValue" | "visible" | "onToggleVisibility">
  >
> = (props) => {
  const { totalValue, visible, onToggleVisibility } = props;
  const { t } = useTranslation();
  const Icon = visible ? EyeIcon : EyeCloseIcon;
  return (
    <Flex direction="column" gap={1} className="oui-text-2xs" itemAlign="start">
      <Flex gap={1} justify="start" itemAlign="center">
        <Text size="2xs" color="neutral" weight="semibold">
          {t("common.totalValue")}
        </Text>
        <button onClick={onToggleVisibility}>
          <Icon size={18} className={cn("oui-text-base-contrast-54")} />
        </button>
      </Flex>
      <Flex justify={"start"} itemAlign="end" gap={1}>
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
        <Text as="div" weight="bold" className="oui-leading-6">
          USDC
        </Text>
      </Flex>
    </Flex>
  );
};

const DepositAndWithdrawButton: React.FC<
  Readonly<
    Pick<AssetsWidgetProps, "isMainAccount" | "onWithdraw" | "onDeposit">
  >
> = (props) => {
  const { t } = useTranslation();
  const { isMainAccount, onWithdraw, onDeposit } = props;
  const { wrongNetwork, disabledConnect } = useAppContext();
  if (!isMainAccount) {
    return null;
  }
  const mergedDisabled = wrongNetwork || disabledConnect;
  return (
    <Flex
      className="oui-text-2xs oui-text-base-contrast-54"
      itemAlign="center"
      gap={3}
    >
      <Button
        disabled={mergedDisabled}
        data-testid="oui-testid-assetView-deposit-button"
        fullWidth
        size="md"
        onClick={onDeposit}
      >
        <ArrowDownShortIcon
          color="white"
          opacity={mergedDisabled ? 0.4 : 1}
          className="oui-rotate-0 oui-text-primary-contrast"
        />
        <Text>{t("common.deposit")}</Text>
      </Button>
      <Button
        fullWidth
        disabled={mergedDisabled}
        color="secondary"
        size="md"
        onClick={onWithdraw}
        data-testid="oui-testid-assetView-withdraw-button"
      >
        <ArrowDownShortIcon
          color="white"
          opacity={mergedDisabled ? 0.4 : 1}
          className="oui-rotate-180 oui-text-base-contrast"
        />
        <Text>{t("common.withdraw")}</Text>
      </Button>
    </Flex>
  );
};

const DataFilterSection: React.FC<
  Pick<
    AssetsWidgetProps,
    | "isMainAccount"
    | "onFilter"
    | "selectedAccount"
    | "selectedAsset"
    | "assetsOptions"
    | "state"
  >
> = (props) => {
  const { t } = useTranslation();
  const {
    isMainAccount,
    onFilter,
    selectedAccount,
    selectedAsset,
    assetsOptions,
    state,
  } = props;

  const ALL_ACCOUNTS: SelectOption = {
    label: t("common.allAccount"),
    value: AccountType.ALL,
  };

  const MAIN_ACCOUNT: SelectOption = {
    label: t("common.mainAccount"),
    value: AccountType.MAIN,
  };

  const ALL_ASSETS: SelectOption = {
    label: t("common.allAssets", "All assets"),
    value: "all",
  };

  const subAccounts = useMemo<SubAccount[]>(() => {
    return state.subAccounts ?? [];
  }, [state.subAccounts]);

  const memoizedOptions = useMemo(() => {
    if (Array.isArray(subAccounts) && subAccounts.length) {
      return [
        ALL_ACCOUNTS,
        MAIN_ACCOUNT,
        ...subAccounts.map<SelectOption>((value) => ({
          value: value?.id,
          label: value?.description || formatAddress(value?.id),
        })),
      ];
    }
    return [ALL_ACCOUNTS, MAIN_ACCOUNT];
  }, [ALL_ACCOUNTS, MAIN_ACCOUNT, subAccounts]);

  const memoizedAssetOptions = useMemo(() => {
    return [ALL_ASSETS, ...assetsOptions];
  }, [ALL_ASSETS, assetsOptions]);

  if (!isMainAccount) {
    return null;
  }

  return (
    <DataFilter
      className="oui-border-none oui-py-0"
      onFilter={onFilter}
      items={[
        {
          type: "select",
          name: "account",
          value: selectedAccount,
          options: memoizedOptions,
        },
        {
          type: "select",
          name: "asset",
          value: selectedAsset,
          options: memoizedAssetOptions,
        },
      ]}
    />
  );
};

export const AssetsDataTable: React.FC<
  Pick<
    AssetsWidgetProps,
    | "columns"
    | "dataSource"
    | "isMainAccount"
    | "onFilter"
    | "selectedAccount"
    | "selectedAsset"
    | "assetsOptions"
    | "state"
    | "canTrade"
  > &
    AssetsDataTableWidgetProps
> = (props) => {
  const { columns, dataSource, dataTableClassNames, classNames } = props;
  const { root, scrollRoot, desc } = classNames ?? {};

  if (!props.canTrade) {
    // show auth button when not can trade
    return (
      <AuthGuardDataTable
        classNames={{
          root: "oui-rounded-xl oui-font-semibold",
        }}
        loading={props.canTrade}
        columns={[]}
        dataSource={[]}
      />
    );
  }

  return (
    <Flex width="100%" direction={"column"} className={root}>
      <DataFilterSection
        {...pick(
          [
            "isMainAccount",
            "onFilter",
            "selectedAccount",
            "selectedAsset",
            "assetsOptions",
            "state",
          ],
          props,
        )}
      />
      {dataSource?.map((item, index) => {
        return (
          <Flex
            key={`item-${index}`}
            className={cn("oui-rounded-xl oui-bg-base-9 oui-p-6", scrollRoot)}
            direction={"column"}
            itemAlign={"start"}
            justify={"between"}
            my={4}
          >
            <Text
              className={cn("oui-mb-4", desc)}
              intensity={98}
              weight="semibold"
              size="lg"
            >
              {item?.description || formatAddress(item?.id ?? "")}
            </Text>
            <AuthGuardDataTable
              bordered
              className="oui-font-semibold"
              classNames={{
                scroll: cn(
                  "oui-h-auto oui-min-h-0 oui-overflow-x-auto oui-overflow-y-visible",
                  dataTableClassNames?.scroll,
                ),
                ...dataTableClassNames,
              }}
              columns={columns}
              dataSource={item.children}
            />
          </Flex>
        );
      })}
    </Flex>
  );
};

export const AssetsTable: React.FC<AssetsWidgetProps> = (props) => {
  const { t } = useTranslation();
  return (
    <Card
      className="oui-border-none oui-bg-transparent oui-p-0 oui-shadow-none"
      classNames={{ content: "!oui-pt-0" }}
    >
      <Tabs
        defaultValue="assets"
        variant="contained"
        classNames={{ tabsList: "" }}
        size="lg"
      >
        <TabPanel value="assets" className="" title={t("common.assets")}>
          <Flex
            className="oui-rounded-xl oui-bg-base-9 oui-p-6"
            direction={"row"}
            itemAlign={"center"}
            justify={"between"}
            my={4}
          >
            <TotalValueInfo
              {...pick(["totalValue", "visible", "onToggleVisibility"], props)}
            />
            <DepositAndWithdrawButton
              {...pick(["isMainAccount", "onDeposit", "onWithdraw"], props)}
            />
          </Flex>
          <AssetsDataTable
            {...pick(
              [
                "columns",
                "dataSource",
                "isMainAccount",
                "onFilter",
                "selectedAccount",
                "selectedAsset",
                "assetsOptions",
                "state",
                "canTrade",
              ],
              props,
            )}
            classNames={{
              scrollRoot:
                "oui-max-h-[700px] oui-overflow-y-auto oui-custom-scrollbar oui-w-full",
            }}
          />
        </TabPanel>
        <TabPanel
          value="convertHistory"
          className="oui-rounded-xl oui-bg-base-9 oui-px-6"
          title={t("portfolio.overview.tab.convert.history")}
        >
          <React.Suspense fallback={null}>
            <Flex
              direction="column"
              mt={4}
              itemAlign="center"
              className="oui-w-full"
            >
              <Divider className="oui-w-full oui-border-b oui-border-line-4 oui-pt-6" />
              <LazyConvertHistoryWidget />
            </Flex>
          </React.Suspense>
        </TabPanel>
      </Tabs>
    </Card>
  );
};
