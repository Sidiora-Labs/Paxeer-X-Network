import { useTranslation } from "@paxeer-network/i18n";
import {
  Box,
  Flex,
  Text,
  ChainIcon,
  Tabs,
  TabPanel,
  cn,
  tv,
} from "@paxeer-network/ui";
import { UseChainSelectorScriptReturn } from "./chainSelector.script";
import { ChainType, TChainItem } from "./type";

export type ChainSelectorProps = {
  isWrongNetwork?: boolean;
  /**
   * wide: This represents the wide screen (desktop) UI mode
   * compact: This indicates a compact (mobile) UI pattern.
   */
  variant?: "wide" | "compact";
  className?: string;
} & UseChainSelectorScriptReturn;

const chainSelectorVariants = tv({
  slots: {
    icon: "",
    list: "oui-grid oui-grid-cols-1 oui-gap-1",
    mainnetList: '"',
    testnetList: '"',
    recentList: "",
    item: "oui-w-full oui-rounded-md",
    tip: "oui-text-center",
  },
  variants: {
    variant: {
      compact: {
        icon: "oui-w-6 oui-h-6",
        list: "oui-bg-base-9 oui-rounded-lg oui-p-1",
        mainnetList: "oui-grid-cols-2 oui-mt-4",
        testnetList: "oui-grid-cols-1 oui-mt-4",
        recentList: "oui-mt-4",
        item: "oui-bg-base-6 hover:oui-bg-base-7",
        tip: "oui-pt-6",
      },
      wide: {
        icon: "oui-w-[18px] oui-h-[18px]",
        mainnetList: "oui-grid-cols-3 oui-mt-3",
        testnetList: "oui-grid-cols-2 oui-mt-3",
        recentList: "oui-mt-3",
        item: "oui-bg-base-5 hover:oui-bg-base-6",
        tip: "oui-pt-8",
      },
    },
    selected: {
      true: {
        item: "",
      },
      false: { item: "oui-bg-transparent" },
    },
  },
  compoundVariants: [
    {
      variant: "compact",
      selected: true,
      className: {
        item: "hover:oui-bg-base-6",
      },
    },
    {
      variant: "wide",
      selected: true,
      className: {
        item: "hover:oui-bg-base-5",
      },
    },
  ],
  defaultVariants: {
    variant: "wide",
    selected: false,
  },
});

//------------------ ChainSelector start ------------------
export const ChainSelector = (props: ChainSelectorProps) => {
  const { isWrongNetwork, variant = "wide" } = props;
  const { t } = useTranslation();
  const { list, recentList, mainnetList, testnetList, icon, item, tip } =
    chainSelectorVariants({ variant });

  return (
    <Box className={cn("oui-font-semibold", props.className)}>
      <Tabs
        value={props.selectedTab}
        variant="contained"
        size={variant === "wide" ? "md" : "lg"}
        onValueChange={(e) => props.onTabChange(e as ChainType)}
      >
        {props.showMainnet && (
          <TabPanel value={ChainType.Mainnet} title={t("connector.mainnet")}>
            {!!props.recentChains?.length && (
              <Flex gap={2} className={recentList()}>
                {props.recentChains?.map((item) => {
                  return (
                    <RecentChainItem
                      key={item.id}
                      item={item}
                      onClick={() => props.onChainClick(item)}
                      iconClassName={icon()}
                    />
                  );
                })}
              </Flex>
            )}

            <Box r="2xl" className={cn(list(), mainnetList())}>
              {props.chains.mainnet?.map((chain) => {
                const selected = props.selectChainId === chain.id;
                return (
                  <ChainItem
                    key={chain.id}
                    selected={selected}
                    item={chain}
                    onClick={() => props.onChainClick(chain)}
                    className={item({ selected })}
                  />
                );
              })}
            </Box>
          </TabPanel>
        )}

        {props.showTestnet && (
          <TabPanel value={ChainType.Testnet} title={t("connector.testnet")}>
            <Box r="2xl" className={cn(list(), testnetList())}>
              {props.chains.testnet?.map((chain) => {
                const selected = props.selectChainId === chain.id;
                return (
                  <ChainItem
                    key={chain.id}
                    selected={selected}
                    item={chain}
                    onClick={() => props.onChainClick(chain)}
                    className={item({ selected })}
                  />
                );
              })}
            </Box>
          </TabPanel>
        )}
      </Tabs>

      {isWrongNetwork && (
        <Box className={tip()}>
          <Text color="warning" size="xs">
            {t("connector.wrongNetwork.tooltip")}
          </Text>
        </Box>
      )}
    </Box>
  );
};
// ------------------ ChainSelector end ------------------

// ------------------ ChainItem start ------------------
export const ChainItem = (props: {
  selected: boolean;
  item: TChainItem;
  onClick?: () => void;
  className?: string;
}) => {
  const { item } = props;
  return (
    <button className={props.className} onClick={props.onClick}>
      <Flex justify="between" className="oui-py-2.5" px={3}>
        <Flex itemAlign="center" width="100%" className="oui-gap-x-[6px]">
          <ChainIcon chainId={item.id} size="xs" />
          <Text size="2xs">{item.name}</Text>
        </Flex>
        {props.selected && (
          <Box gradient="brand" r="full" width={4} height={4} />
        )}
      </Flex>
    </button>
  );
};

// ------------------ Recent ChainItem start ------------------
export const RecentChainItem = (props: {
  item: TChainItem;
  onClick?: () => void;
  iconClassName?: string;
}) => {
  return (
    <button
      className="oui-border oui-border-line-12 oui-rounded-lg hover:oui-border-primary-light"
      onClick={props.onClick}
    >
      <Flex itemAlign="center" p={2}>
        <ChainIcon chainId={props.item.id} className={props.iconClassName} />
      </Flex>
    </button>
  );
};
