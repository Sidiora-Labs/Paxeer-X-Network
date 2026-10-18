import { FC, useState } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import {
  ArrowDownSquareFillIcon,
  ArrowUpSquareFillIcon,
  registerSimpleDialog,
  registerSimpleSheet,
  TabPanel,
  Tabs,
} from "@paxeer-network/ui";
import { VaultDepositWidget } from "../deposit";
import { VaultWithdrawWidget } from "../withdraw";

export const VaultDepositAndWithdrawWithDialogId =
  "VaultDepositAndWithdrawWithDialogId";
export const VaultDepositAndWithdrawWithSheetId =
  "VaultDepositAndWithdrawWithSheetId";

export type VaultDepositAndWithdrawProps = {
  activeTab?: "deposit" | "withdraw";
  close?: () => void;
  vaultId: string;
};

export const VaultDepositAndWithdraw: FC<VaultDepositAndWithdrawProps> = (
  props,
) => {
  const [activeTab, setActiveTab] = useState<string>(
    props.activeTab || "deposit",
  );
  const { vaultId } = props;
  const { t } = useTranslation();

  return (
    <Tabs
      value={activeTab}
      onValueChange={setActiveTab}
      variant="contained"
      size="lg"
      classNames={{
        tabsList: "oui-px-0",
        tabsContent: "oui-pt-5",
      }}
    >
      <TabPanel
        title={t("common.deposit")}
        icon={<ArrowDownSquareFillIcon />}
        value="deposit"
      >
        <VaultDepositWidget vaultId={vaultId} />
      </TabPanel>
      <TabPanel
        title={t("common.withdraw")}
        icon={<ArrowUpSquareFillIcon />}
        value="withdraw"
      >
        <VaultWithdrawWidget vaultId={vaultId} />
      </TabPanel>
    </Tabs>
  );
};

registerSimpleDialog(
  VaultDepositAndWithdrawWithDialogId,
  VaultDepositAndWithdraw,
  {
    size: "md",
    classNames: {
      content: "oui-border oui-border-line-6",
    },
  },
);

registerSimpleSheet(
  VaultDepositAndWithdrawWithSheetId,
  VaultDepositAndWithdraw,
);
