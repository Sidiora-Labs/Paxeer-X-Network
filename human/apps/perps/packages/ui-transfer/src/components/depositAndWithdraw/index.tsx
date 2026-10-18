import { FC, isValidElement, useState } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { injectable } from "@paxeer-network/plugin-core";
import {
  ArrowDownSquareFillIcon,
  ArrowUpSquareFillIcon,
  registerSimpleDialog,
  registerSimpleSheet,
  TabPanel,
  Tabs,
} from "@paxeer-network/ui";
import { DepositSlot } from "./depositSlot";
import { WithdrawSlot } from "./withdrawSlot";

export const DepositAndWithdrawWithDialogId = "DepositAndWithdrawWithDialogId";
export const DepositAndWithdrawWithSheetId = "DepositAndWithdrawWithSheetId";

export interface DepositTabExtension {
  id: string;
  title: string;
  icon: React.ReactNode;
  component: React.ComponentType<{ close?: () => void }>;
  order?: number;
}

export type DepositAndWithdrawProps = {
  activeTab?: string;
  close?: () => void;
  extraTabs?: DepositTabExtension[];
};

export const DepositAndWithdraw: FC<DepositAndWithdrawProps> = (props) => {
  const { extraTabs = [] } = props;
  const [activeTab, setActiveTab] = useState<string>(
    props.activeTab || "deposit",
  );
  const { t } = useTranslation();
  const sortedExtra = [...extraTabs].sort(
    (a, b) => (a.order ?? 100) - (b.order ?? 100),
  );

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
        <DepositSlot close={props.close} />
        {/* <DepositFormWidget close={props.close} /> */}
      </TabPanel>
      <TabPanel
        title={t("common.withdraw")}
        icon={<ArrowUpSquareFillIcon />}
        value="withdraw"
      >
        <WithdrawSlot close={props.close} />
        {/* <WithdrawFormWidget close={props.close} /> */}
      </TabPanel>
      {sortedExtra.map((tab) => (
        <TabPanel
          key={tab.id}
          title={tab.title}
          icon={isValidElement(tab.icon) ? tab.icon : undefined}
          value={tab.id}
        >
          <tab.component close={props.close} />
        </TabPanel>
      ))}
    </Tabs>
  );
};

export const InjectableDepositAndWithdraw = injectable(
  DepositAndWithdraw,
  "Transfer.DepositAndWithdraw",
);

registerSimpleDialog(
  DepositAndWithdrawWithDialogId,
  InjectableDepositAndWithdraw,
  {
    size: "md",
    classNames: {
      content: "oui-border oui-border-line-6",
    },
  },
);

registerSimpleSheet(
  DepositAndWithdrawWithSheetId,
  InjectableDepositAndWithdraw,
);
