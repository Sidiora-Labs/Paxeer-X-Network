import React from "react";
import { useLocalStorage } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { TabPanel, Tabs } from "@paxeer-network/ui";
import { useCommunityTabs } from "../../hooks/useCommunityTabs";

export type CommunityBrokerTabsProps = {
  storageKey: string;
  className?: string;
  classNames?: Record<string, string>;
  variant?: any;
  size?: any;
  showScrollIndicator?: boolean;
  allTitle?: React.ReactNode;
  renderPanel: (selectedId: string) => React.ReactNode;
};

export const CommunityBrokerTabs: React.FC<CommunityBrokerTabsProps> = (
  props,
) => {
  const {
    storageKey,
    className,
    classNames,
    variant = "contained",
    size = "sm",
    showScrollIndicator,
    allTitle,
    renderPanel,
  } = props;

  const { t } = useTranslation();
  const [communitySubTab, setCommunitySubTab] = useLocalStorage<string>(
    storageKey,
    "all",
  );
  const communityBrokerTabs = useCommunityTabs();

  // Keep a TabPanel for the persisted selection while broker tabs are still
  // loading, so remounting ExpandMarkets does not force the value back to "all".
  const selectedBrokerTab =
    communitySubTab !== "all" &&
    !communityBrokerTabs.some((b) => b.id === communitySubTab)
      ? { id: communitySubTab, label: communitySubTab }
      : null;

  return (
    <Tabs
      variant={variant}
      size={size}
      value={communitySubTab}
      onValueChange={setCommunitySubTab}
      classNames={classNames as any}
      className={className}
      showScrollIndicator={showScrollIndicator}
    >
      <TabPanel title={allTitle ?? t("common.all")} value="all">
        {renderPanel("all")}
      </TabPanel>
      {selectedBrokerTab ? (
        <TabPanel
          key={selectedBrokerTab.id}
          title={selectedBrokerTab.label}
          value={selectedBrokerTab.id}
        >
          {renderPanel(selectedBrokerTab.id)}
        </TabPanel>
      ) : null}
      {communityBrokerTabs.map((b) => (
        <TabPanel key={b.id} title={b.label} value={b.id}>
          {renderPanel(b.id)}
        </TabPanel>
      ))}
    </Tabs>
  );
};
