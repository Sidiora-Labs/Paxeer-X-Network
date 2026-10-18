import React from "react";
import { TabPanel, injectable } from "@paxeer-network/ui";

/** Interceptor target for the desktop data-list tabs; plugins append custom tabs after the built-in ones. */
export const DataListDesktopTabsTarget = "Trading.DataList.Desktop.Tabs";

export interface DataListDesktopTabItem {
  /** Unique tab id (also its `value` in the Tabs context). */
  id: string;
  title: React.ReactNode;
  content: React.ReactNode;
}

export interface DataListDesktopTabsProps {
  items: DataListDesktopTabItem[];
}

// Renders a TabPanel per item. TabPanel self-registers into the parent Tabs,
// so appended tabs show up as triggers next to the built-in ones.
const DataListDesktopTabs: React.FC<DataListDesktopTabsProps> = ({ items }) => (
  <>
    {items.map((tab) => (
      <TabPanel key={tab.id} value={tab.id} title={tab.title}>
        {tab.content}
      </TabPanel>
    ))}
  </>
);

export const InjectableDataListDesktopTabs =
  injectable<DataListDesktopTabsProps>(
    DataListDesktopTabs,
    DataListDesktopTabsTarget,
  );
