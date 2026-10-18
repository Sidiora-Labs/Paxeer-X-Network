import React from "react";
import { TabPanel, injectable } from "@paxeer-network/ui";

/** Interceptor target for the mobile data-list tabs; plugins append custom tabs after the built-in ones. */
export const DataListMobileTabsTarget = "Trading.DataList.Mobile.Tabs";

export interface DataListMobileTabItem {
  /** Unique tab id (also its `value` in the Tabs context). */
  id: string;
  title: React.ReactNode;
  content: React.ReactNode;
}

export interface DataListMobileTabsProps {
  items: DataListMobileTabItem[];
}

// Renders a TabPanel per item. TabPanel self-registers into the parent Tabs,
// so appended tabs show up as triggers next to the built-in ones.
const DataListMobileTabs: React.FC<DataListMobileTabsProps> = ({ items }) => (
  <>
    {items.map((tab) => (
      <TabPanel key={tab.id} value={tab.id} title={tab.title}>
        {tab.content}
      </TabPanel>
    ))}
  </>
);

export const InjectableDataListMobileTabs = injectable<DataListMobileTabsProps>(
  DataListMobileTabs,
  DataListMobileTabsTarget,
);
