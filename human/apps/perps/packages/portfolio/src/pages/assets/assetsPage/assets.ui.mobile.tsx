/* eslint-disable @typescript-eslint/no-explicit-any */
import React, { useMemo } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import {
  Button,
  cn,
  TokenIcon,
  Text,
  formatAddress,
  DataFilter,
  modal,
  Flex,
  EmptyDataState,
} from "@paxeer-network/ui";
import { SelectOption } from "@paxeer-network/ui/src/select/withOptions";
import type { useAssetsScriptReturn } from "./assets.script";
import { AccountType } from "./assets.ui.desktop";

const AccountTag: React.FC<{ name: string }> = ({ name }) => {
  return (
    <div className="oui-mt-2 oui-flex oui-h-[18px] oui-w-fit oui-items-center oui-rounded oui-bg-base-contrast-6 oui-px-2 oui-text-2xs oui-font-semibold oui-text-base-contrast-36">
      {name}
    </div>
  );
};

type AssetMobileItemProps = {
  item: any;
};

const AssetMobileItem: React.FC<AssetMobileItemProps> = (props) => {
  const { item } = props;
  const { t } = useTranslation();
  return (
    <div className="oui-flex oui-flex-col oui-gap-2 oui-rounded-xl oui-bg-base-9 oui-p-2">
      <Flex
        width={"100%"}
        justify={"between"}
        itemAlign={"center"}
        className="oui-gap-x-0.5"
      >
        <Flex
          className="oui-w-1/3 oui-truncate"
          itemAlign={"start"}
          direction={"column"}
        >
          <div className="oui-text-2xs oui-font-semibold oui-text-base-contrast-36">
            {t("common.token")}
          </div>
          <Flex
            gap={1}
            justify={"start"}
            itemAlign={"center"}
            className="oui-text-xs oui-font-semibold oui-text-base-contrast-80"
          >
            <TokenIcon name={item.token} size="2xs" />
            {item.token}
          </Flex>
        </Flex>
        <Flex
          className="oui-w-1/3 oui-truncate"
          itemAlign={"start"}
          direction={"column"}
        >
          <div className="oui-text-2xs oui-font-semibold oui-text-base-contrast-36">
            {t("common.qty")}
          </div>
          <Text.numeral
            className="oui-truncate"
            size="xs"
            dp={6}
            padding={false}
          >
            {item.holding}
          </Text.numeral>
        </Flex>
        <Flex
          className="oui-w-1/3 oui-truncate"
          itemAlign={"start"}
          direction={"column"}
        >
          <div className="oui-w-full oui-text-end oui-text-2xs oui-font-semibold oui-text-base-contrast-36">
            {t("common.indexPrice")}
          </div>
          <Flex
            gap={1}
            width={"100%"}
            justify={"end"}
            itemAlign={"center"}
            className="oui-text-end oui-font-semibold oui-text-base-contrast-80"
          >
            <Text.numeral
              size="xs"
              rule="price"
              dp={6}
              currency="$"
              padding={false}
            >
              {item.indexPrice}
            </Text.numeral>
            <div className="oui-text-end oui-text-2xs oui-text-base-contrast-36">
              USDC
            </div>
          </Flex>
        </Flex>
      </Flex>
      <Flex
        width={"100%"}
        justify={"between"}
        itemAlign={"center"}
        className="oui-gap-x-0.5"
      >
        <Flex
          className="oui-w-1/3 oui-truncate"
          itemAlign={"start"}
          direction={"column"}
        >
          <div className="oui-text-2xs oui-font-semibold oui-text-base-contrast-36">
            {t("portfolio.overview.column.assetValue")}
          </div>
          <Text.assetValue
            size="xs"
            intensity={80}
            className="oui-truncate oui-font-semibold"
            rule="price"
            dp={6}
            currency="$"
            padding={false}
          >
            {item.assetValue}
          </Text.assetValue>
        </Flex>
        <Flex
          className="oui-w-1/3 oui-truncate"
          itemAlign={"start"}
          direction={"column"}
        >
          <div className="oui-text-2xs oui-font-semibold oui-text-base-contrast-36">
            {t("portfolio.overview.column.collateralRatio")}
          </div>
          <Text.numeral size="xs" dp={2} suffix="%">
            {item.collateralRatio * 100}
          </Text.numeral>
        </Flex>
        <Flex
          className="oui-w-1/3 oui-truncate"
          itemAlign={"start"}
          direction={"column"}
        >
          <div className="oui-w-full oui-text-end oui-text-2xs oui-font-semibold oui-text-base-contrast-36">
            {t("transfer.deposit.collateralContribution")}
          </div>
          <Flex
            gap={1}
            width={"100%"}
            justify={"end"}
            itemAlign={"center"}
            className="oui-text-end oui-font-semibold oui-text-base-contrast-80"
          >
            <Text.collateral
              size="xs"
              rule="price"
              dp={6}
              currency="$"
              padding={false}
            >
              {item.collateralContribution}
            </Text.collateral>
            <div className="oui-text-end oui-text-2xs oui-text-base-contrast-36">
              USDC
            </div>
          </Flex>
        </Flex>
      </Flex>
      <Flex justify={"between"} itemAlign={"center"} gap={2}>
        {item.token !== "USDC" && (
          <Button
            fullWidth
            variant="outlined"
            size="sm"
            color="gray"
            onClick={() => {
              modal.show("ConvertSheetId", {
                accountId: item.account_id,
                token: item.token,
              });
            }}
            className={cn(
              "oui-flex-1 oui-border-base-contrast-36 oui-text-base-contrast-54",
            )}
          >
            {t("transfer.convert")}
          </Button>
        )}
        <Button
          fullWidth
          variant="outlined"
          size="sm"
          color="gray"
          onClick={() => {
            modal.show("TransferSheetId", {
              accountId: item.account_id,
              token: item.token,
            });
          }}
          className={cn(
            "oui-flex-1 oui-border-base-contrast-36 oui-text-base-contrast-54",
          )}
        >
          {t("common.transfer")}
        </Button>
      </Flex>
    </div>
  );
};

export const AssetsTableMobile: React.FC<useAssetsScriptReturn> = (props) => {
  const { t } = useTranslation();

  const {
    assetsOptions,
    state,
    isMainAccount,
    dataSource,
    selectedAccount,
    selectedAsset,
    onFilter,
  } = props;

  const subAccounts = state.subAccounts ?? [];

  const ALL_ACCOUNTS: SelectOption = {
    label: t("common.allAccount"),
    value: AccountType.ALL,
  };

  const MAIN_ACCOUNT: SelectOption = {
    label: t("common.mainAccount"),
    value: AccountType.MAIN,
  };

  const ALL_ASSETS: SelectOption = {
    label: t("common.allAssets"),
    value: "all",
  };

  const memoizedOptions = useMemo(() => {
    if (Array.isArray(subAccounts) && subAccounts.length) {
      return [
        ALL_ACCOUNTS,
        MAIN_ACCOUNT,
        ...subAccounts.map<SelectOption>((value) => ({
          value: value.id,
          label: value?.description || formatAddress(value?.id),
        })),
      ];
    }
    return [ALL_ACCOUNTS, MAIN_ACCOUNT];
  }, [subAccounts]);

  // Create asset options from holding data - optimized and consistent with desktop
  const memoizedAssets = useMemo(() => {
    return [ALL_ASSETS, ...assetsOptions];
  }, [assetsOptions]);

  if (!props.canTrade) {
    return (
      <Flex
        direction={"column"}
        height={"100%"}
        itemAlign={"center"}
        justify={"center"}
        mt={10}
      >
        <EmptyDataState />
      </Flex>
    );
  }

  return (
    <div className={cn("oui-flex oui-flex-col oui-gap-1 oui-px-1 oui-pb-4")}>
      {isMainAccount && (
        <DataFilter
          onFilter={onFilter}
          className="oui-border-none oui-py-2"
          items={[
            {
              size: "sm",
              type: "picker",
              name: "account",
              value: selectedAccount,
              options: memoizedOptions,
            },
            {
              size: "sm",
              type: "picker",
              name: "asset",
              value: selectedAsset,
              options: memoizedAssets,
            },
          ]}
        />
      )}
      <div className="oui-flex oui-flex-col oui-gap-1">
        {dataSource?.map((assets, index) => {
          return (
            <React.Fragment key={`item-${index}`}>
              <AccountTag name={assets.description ?? "sub account"} />
              {Array.isArray(assets.children) &&
                assets.children.map((child) => (
                  <AssetMobileItem
                    item={child}
                    key={`${child.token}-${child.account_id}`}
                  />
                ))}
            </React.Fragment>
          );
        })}
      </div>
    </div>
  );
};
