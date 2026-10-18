import { PropsWithoutRef, useEffect, useMemo, useState } from "react";
import * as SelectPrimitive from "@radix-ui/react-select";
import { cnBase, VariantProps } from "tailwind-variants";
import { Box } from "../box";
import { Flex } from "../flex";
import { ChainIcon } from "../icon";
import { Tabs, TabPanel } from "../tabs";
import { Text } from "../typography";
import { tv } from "../utils/tv";
import { selectVariants } from "./selectPrimitive";

const chainSelectVariants = tv({
  extend: selectVariants,
  slots: {
    icon: "",
    item: [
      "oui-text-2xs",
      "oui-rounded-md",
      "hover:oui-rounded-md",
      "oui-cursor-pointer",
    ],
    tag: "oui-bg-success/20 oui-text-success oui-px-2 oui-rounded oui-font-semibold",
  },
  variants: {
    size: {
      xs: {
        icon: "",
      },
      sm: {
        icon: "",
      },
      md: {
        icon: "oui-w-5 oui-h-5",
        item: "oui-px-4 oui-py-2 oui-h-6 oui-box-content",
      },
      lg: {
        icon: "oui-w-6 oui-h-6",
      },
      xl: {
        icon: "oui-w-7 oui-h-7",
      },
    },
  },
  defaultVariants: {
    size: "md",
  },
});
const ChainSelectorType = {
  Mainnet: "Mainnet",
  Testnet: "Testnet",
};

type ChainItem = {
  name: string;
  id: number;
  lowestFee?: boolean;
};

type ChainSelectProps = {
  value?: number;
  onChange?: (chain: ChainItem) => void;
  storageChains?: ChainItem[];
  networkId?: string;
  chains: {
    mainnet: ChainItem[];
    testnet: ChainItem[];
  };
  contentProps?: PropsWithoutRef<typeof SelectPrimitive.Content>;
} & VariantProps<typeof chainSelectVariants> &
  PropsWithoutRef<typeof SelectPrimitive.Root>;

function ChainSelectItem(props: {
  chain: ChainItem;
  itemClassName: string;
  iconClassName: string;
}) {
  return (
    <SelectPrimitive.SelectItem
      value={`${props.chain.id}`}
      className={cnBase(props.itemClassName)}
    >
      <Flex itemAlign={"center"} justify={"between"} width={"100%"}>
        <Flex gap={2} itemAlign={"center"}>
          <ChainIcon chainId={props.chain.id} size="xs" />
          <Text size="xs">{props.chain.name}</Text>
        </Flex>
        <SelectPrimitive.ItemIndicator>
          <Box width={"6px"} height={"6px"} gradient={"brand"} r={"full"} />
        </SelectPrimitive.ItemIndicator>
      </Flex>
    </SelectPrimitive.SelectItem>
  );
}

const RecommandChain = (props: {
  selected: boolean;
  item: ChainItem;
  className?: string;
}) => {
  const { item } = props;
  return (
    <SelectPrimitive.SelectItem value={`${item.id}`}>
      <div
        className={cnBase(
          "oui-border oui-border-line-12 oui-rounded-md hover:oui-border-primary-light",
          // props.selected && "oui-border-primary-light",
          props.className,
        )}
      >
        <Flex justify="between">
          <Flex itemAlign="center" width="100%" p={2} gap={1}>
            <ChainIcon
              chainId={item.id}
              className="oui-w-[18px] oui-h-[18px]"
            />
          </Flex>
        </Flex>
      </div>
    </SelectPrimitive.SelectItem>
  );
};

const ChainSelect = (props: ChainSelectProps) => {
  const {
    chains = {
      mainnet: [],
      testnet: [],
    },
    size,
    error,
    variant,
    position,
    contentProps,
    value,
    storageChains,
    ...rest
  } = props;

  const mergedChains = useMemo(() => {
    return [...chains.mainnet, ...chains.testnet];
  }, [chains]);

  const { trigger, icon, content, item, viewport, tag } = chainSelectVariants({
    size,
    variant,
    error,
  });

  const [currentChain, setCurrentChain] = useState<number | undefined>(
    props.value,
  );

  const [selectedTab, setSelectedTab] = useState(ChainSelectorType.Mainnet);

  useEffect(() => {
    if (props.value !== currentChain) {
      setCurrentChain(props.value);
    }
  }, [props.value]);

  const onChange = (value: any) => {
    if (!chains || !Array.isArray(mergedChains)) return;
    const selected = mergedChains.find((chain) => chain.id === Number(value));
    setCurrentChain(selected?.id);
    if (!selected) return;
    props.onChange?.(selected);
  };

  const onTabChange = (tab: string) => {
    setSelectedTab(tab);
  };

  useEffect(() => {
    if (currentChain) {
      const isMainnet = props.chains.mainnet?.some(
        (chain) => chain.id === currentChain,
      );
      const isTestnet = props.chains.testnet?.some(
        (chain) => chain.id === currentChain,
      );
      if (isMainnet) {
        onTabChange(ChainSelectorType.Mainnet);
      } else if (isTestnet) {
        onTabChange(ChainSelectorType.Testnet);
      }
    }
  }, [currentChain, props.chains]);

  return (
    <SelectPrimitive.Root
      {...rest}
      value={`${currentChain}`}
      onValueChange={onChange}
    >
      <SelectPrimitive.Trigger className={trigger()} asChild>
        <button className="oui-relative oui-px-3 oui-box-border oui-min-w-11">
          {!!currentChain && (
            <ChainIcon chainId={currentChain} className={icon()} />
          )}
          <svg
            width="10"
            height="10"
            viewBox="0 0 10 10"
            xmlns="http://www.w3.org/2000/svg"
            className="oui-absolute oui-end-0 oui-bottom-0"
          >
            <defs>
              <linearGradient
                id="paint0_linear_490_5080"
                x1="10"
                y1="5"
                x2="-5.79673e-08"
                y2="5"
                gradientUnits="userSpaceOnUse"
              >
                <stop stopColor="rgb(var(--oui-gradient-brand-end))" />
                <stop
                  offset="1"
                  stopColor="rgb(var(--oui-gradient-brand-start))"
                />
              </linearGradient>
            </defs>
            <path
              d="M10 7V0L0 10H7C8.65685 10 10 8.65685 10 7Z"
              fill="url(#paint0_linear_490_5080)"
            />
          </svg>
        </button>
      </SelectPrimitive.Trigger>
      <SelectPrimitive.Portal>
        <SelectPrimitive.Content
          position={"popper"}
          className={content({
            className: cnBase(
              "oui-bg-base-8 oui-w-[456px] oui-rounded-xl",
              "oui-border oui-border-line-6",
              "oui-font-semibold",
            ),
          })}
          sideOffset={4}
          collisionPadding={{ left: 16, right: 16 }}
          onCloseAutoFocus={(e) => {
            e.preventDefault();
          }}
          {...contentProps}
        >
          <SelectPrimitive.Viewport className="oui-p-4">
            <Tabs
              value={selectedTab}
              variant="contained"
              size="md"
              onValueChange={(e) => onTabChange(e)}
            >
              <TabPanel
                value={ChainSelectorType.Mainnet}
                title={ChainSelectorType.Mainnet}
              >
                {!!props.storageChains?.length && (
                  <Flex gap={2} className="oui-mt-3">
                    {props.storageChains?.map((item) => {
                      return (
                        <RecommandChain
                          item={item}
                          key={item.id}
                          selected={currentChain === item.id}
                        />
                      );
                    })}
                  </Flex>
                )}

                <div
                  className={cnBase(
                    "oui-grid oui-grid-cols-1 xl:oui-grid-cols-3",
                    "oui-mt-3 oui-gap-1",
                  )}
                >
                  {props.chains.mainnet?.map((chain) => {
                    return (
                      <ChainSelectItem
                        key={chain.id}
                        chain={chain}
                        itemClassName={item()}
                        iconClassName={icon()}
                      />
                    );
                  })}
                </div>
              </TabPanel>
              <TabPanel
                value={ChainSelectorType.Testnet}
                title={ChainSelectorType.Testnet}
              >
                <div
                  className={cnBase(
                    "oui-grid oui-grid-cols-1 xl:oui-grid-cols-2",
                    "oui-mt-3 oui-gap-1",
                  )}
                >
                  {props.chains.testnet?.map((chain) => {
                    return (
                      <ChainSelectItem
                        key={chain.id}
                        chain={chain}
                        itemClassName={item()}
                        iconClassName={icon()}
                      />
                    );
                  })}
                </div>
              </TabPanel>
            </Tabs>
          </SelectPrimitive.Viewport>
        </SelectPrimitive.Content>
      </SelectPrimitive.Portal>
    </SelectPrimitive.Root>
  );
};

ChainSelect.displayName = "ChainSelect";

export { ChainSelect };
export type { ChainSelectProps };
