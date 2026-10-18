import React, {
  FC,
  PropsWithChildren,
  createContext,
  useEffect,
  ReactNode,
  useState,
  useContext,
  ReactElement,
  useMemo,
  useCallback,
} from "react";
import * as TabsPrimitive from "@radix-ui/react-tabs";
import { cnBase, VariantProps } from "tailwind-variants";
import { Flex } from "../flex";
import { useDocumentDirection } from "../hooks";
import { usePaxeerNetworkTheme } from "../provider/paxeerNetworkThemeContext";
import { ScrollIndicator } from "../scrollIndicator";
import {
  TabsBase,
  TabsList,
  TabsContent,
  TabsTrigger,
  tabsVariants,
} from "./tabsBase";

type tabConfig = {
  title: ReactNode;
  icon?: ReactElement;
  testid?: string;
  value: string;
  content: ReactNode;
  collapsed?: boolean;
  className?: string;
  classNames?: {
    content?: string;
    trigger?: string;
  };
  style?: React.CSSProperties;
};

interface TabsContextState {
  registerTab: (tab: tabConfig) => void;
  unregisterTab: (tab: tabConfig) => void;
}

const TabsContext = createContext<TabsContextState>({} as TabsContextState);

function useResolvedTabsDir(
  propDir: React.ComponentProps<typeof TabsPrimitive.Root>["dir"],
): NonNullable<React.ComponentProps<typeof TabsPrimitive.Root>["dir"]> {
  const fromDocument = useDocumentDirection();
  return propDir ?? fromDocument;
}

type TabsProps<T = string> = {
  defaultValue?: T;
  value?: T;
  // onChange?: (value: T) => void;
  leading?: ReactNode;
  trailing?: ReactNode;
  classNames?: {
    tabsListContainer?: string;
    tabsList?: string;
    tabsContent?: string;
    scrollIndicator?: string;
    trigger?: string;
  };
  contentVisible?: boolean;
  showScrollIndicator?: boolean;
} & TabsPrimitive.TabsProps &
  VariantProps<typeof tabsVariants>;

const Tabs: FC<TabsProps> = (props) => {
  const { getComponentTheme } = usePaxeerNetworkTheme();
  const {
    classNames,
    contentVisible = true,
    variant,
    showScrollIndicator,
    value,
    dir: dirProp,
    ...rest
  } = props;

  /** Tab bar layout + keyboard nav: honor explicit `dir` or fall back to the document (`html`). */
  const dir = useResolvedTabsDir(dirProp);

  const tabsOverrides = getComponentTheme("tabs", {
    variant: variant ?? "contained",
  });

  const tabsVariant = tabsOverrides.variant;

  const [tabList, setTabList] = useState<{ [key: string]: tabConfig }>({});

  const registerTab = useCallback((config: tabConfig) => {
    setTabList((prev) => {
      return { ...prev, [config.value]: config };
    });
  }, []);

  const unregisterTab = useCallback((config: tabConfig) => {
    setTabList((prev) => {
      const newTabList = { ...prev };
      delete newTabList[config.value];
      return newTabList;
    });
  }, []);

  const memoizedValue = useMemo<TabsContextState>(
    () => ({ registerTab, unregisterTab }),
    [registerTab, unregisterTab],
  );

  const renderTabsList = () => {
    const tabsList = (
      <TabsList
        variant={tabsVariant}
        size={rest.size}
        className={cnBase("oui-flex-1 oui-border-0", classNames?.tabsList)}
      >
        {Object.keys(tabList).map((key) => {
          const tab = tabList[key];
          return (
            <TabsTrigger
              key={key}
              value={tab.value}
              icon={tab.icon}
              variant={tabsVariant}
              size={rest.size}
              data-testid={tab.testid}
              className={cnBase(classNames?.trigger, tab.classNames?.trigger)}
            >
              {tab.title}
            </TabsTrigger>
          );
        })}
      </TabsList>
    );

    if (showScrollIndicator) {
      return (
        <ScrollIndicator className={classNames?.scrollIndicator}>
          {tabsList}
        </ScrollIndicator>
      );
    }

    return tabsList;
  };

  return (
    <TabsContext.Provider value={memoizedValue}>
      {props.children}
      <TabsBase value={value} {...rest} dir={dir}>
        <Flex
          justify="between"
          itemAlign="center"
          width="100%"
          className={cnBase(
            tabsVariant !== "contained" && "oui-border-b oui-border-b-line-6",
            classNames?.tabsListContainer,
          )}
        >
          {props.leading}
          {renderTabsList()}
          {props.trailing}
        </Flex>
        {contentVisible &&
          Object.keys(tabList).map((key) => {
            const tab = tabList[key];
            return (
              <TabsContent
                key={key}
                value={tab.value}
                className={cnBase(classNames?.tabsContent, tab.className)}
                style={tab.style}
              >
                {tab.content}
              </TabsContent>
            );
          })}
      </TabsBase>
    </TabsContext.Provider>
  );
};

Tabs.displayName = "Tabs";

export interface TabPanelProps {
  value: any;
  title: string | React.ReactNode;
  icon?: React.ReactElement;
  testid?: string;
  className?: string;
  classNames?: {
    content?: string;
    trigger?: string;
  };
  style?: React.CSSProperties;
}

const TabPanel: FC<PropsWithChildren<TabPanelProps>> = (props) => {
  const { title, value, icon, style, testid, children, className, classNames } =
    props;

  const { registerTab, unregisterTab } = useContext(TabsContext);

  useEffect(() => {
    const tabConfig = {
      title,
      value,
      icon,
      testid,
      className,
      classNames,
      style,
      content: children,
    };
    registerTab(tabConfig);
    return () => {
      unregisterTab(tabConfig);
    };
  }, [children, style, icon, testid, title, value, className, classNames]);

  return null;
};

if (process.env.NODE_ENV !== "production") {
  TabPanel.displayName = "TabPanel";
}

export { Tabs, TabPanel };
