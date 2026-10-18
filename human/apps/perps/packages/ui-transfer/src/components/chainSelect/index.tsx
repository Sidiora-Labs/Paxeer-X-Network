import { useState } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { API } from "@paxeer-network/types";
import {
  Badge,
  Box,
  ChainIcon,
  DropdownMenuContent,
  DropdownMenuPortal,
  DropdownMenuRoot,
  DropdownMenuTrigger,
  Flex,
  ScrollArea,
  Spinner,
  Text,
  cn,
  useDocumentDirection,
} from "@paxeer-network/ui";
import { ExchangeIcon } from "../../icons";
import { CurrentChain } from "../../types";

type ChainSelectProps = {
  chains: API.NetworkInfos[];
  value: CurrentChain;
  onValueChange: (chain: API.NetworkInfos) => Promise<void>;
  wrongNetwork: boolean;
  loading?: boolean;
  disabled?: boolean;
};

export const ChainSelect: React.FC<ChainSelectProps> = (props) => {
  const { chains, value, wrongNetwork, loading, disabled } = props;
  const [open, setOpen] = useState(false);
  const { t } = useTranslation();
  const dir = useDocumentDirection();
  const isRTL = dir === "rtl";

  const selectable = wrongNetwork || chains?.length > 1;

  const chainIcon = wrongNetwork ? (
    <Flex
      width={18}
      height={18}
      intensity={100}
      r="full"
      justify="center"
      itemAlign="center"
    >
      <Text size="2xs" intensity={80}>
        U
      </Text>
    </Flex>
  ) : (
    <ChainIcon className="oui-size-[18px]" chainId={value?.id} />
  );

  const chainName = wrongNetwork ? "Unknown" : value?.info?.network_infos?.name;

  const currentChain = chains.find((chain) => chain.chain_id === value?.id);
  const extendedCurrentChain = currentChain as API.NetworkInfos & {
    isSupported?: boolean;
  };
  const isCurrentChainSupported =
    !currentChain || extendedCurrentChain?.isSupported !== false;

  const renderRightIcon = () => {
    if (loading) {
      return <Spinner size="sm" />;
    }
    if (selectable) {
      return <ExchangeIcon className="oui-text-base-contrast-54" />;
    }
  };

  const trigger = (
    <Flex
      className={cn(
        "oui-rounded-t-xl oui-rounded-b-sm oui-bg-base-6",
        disabled
          ? "oui-cursor-not-allowed"
          : selectable
            ? "oui-cursor-pointer"
            : "oui-cursor-auto",
      )}
      height={54}
      px={3}
      justify="between"
      itemAlign="center"
    >
      <div>
        <Flex>
          <Text size="2xs" intensity={54}>
            {t("transfer.network")}
          </Text>
        </Flex>
        <Flex
          direction={isRTL ? "rowReverse" : "row"}
          gapX={1}
          itemAlign="center"
        >
          {chainIcon}
          <Text size="sm" intensity={80}>
            {chainName}
          </Text>
          {!isCurrentChainSupported && (
            <Badge color="danger" size="xs">
              {t("common.notSupported")}
            </Badge>
          )}
        </Flex>
      </div>
      {renderRightIcon()}
    </Flex>
  );

  const content = chains.map((chain, index) => {
    const extendedChain = chain as API.NetworkInfos & {
      isSupported?: boolean;
    };
    const isActive = chain.chain_id === value?.id;
    const isSupported = extendedChain.isSupported !== false;
    return (
      <Flex
        key={chain.chain_id}
        px={2}
        r="base"
        direction={isRTL ? "rowReverse" : "row"}
        justify="between"
        itemAlign="center"
        className={cn(
          "oui-deposit-network-select-item",
          "oui-h-[30px]",
          isSupported
            ? "hover:oui-bg-base-5 oui-cursor-pointer"
            : "oui-cursor-not-allowed",
          isActive && "oui-bg-base-5",
          index !== 0 && "oui-mt-[2px]",
        )}
        onClick={async () => {
          if (!isSupported) return;
          setOpen(false);
          await props.onValueChange(chain);
        }}
      >
        <Flex
          direction={isRTL ? "rowReverse" : "row"}
          gapX={1}
          itemAlign="center"
        >
          <ChainIcon
            className={cn("oui-size-[18px]", !isSupported && "oui-opacity-50")}
            chainId={chain.chain_id}
          />
          <Text size="2xs" intensity={isSupported ? 54 : 36}>
            {chain.name}
          </Text>
          {!isSupported && (
            <Badge color="danger" size="xs">
              {t("common.notSupported")}
            </Badge>
          )}
        </Flex>
        {isActive && (
          <Box
            width={4}
            height={4}
            r="full"
            className="oui-deposit-network-select-active-dot oui-gradient-brand"
          />
        )}
      </Flex>
    );
  });

  return (
    <DropdownMenuRoot open={selectable ? open : false} onOpenChange={setOpen}>
      <DropdownMenuTrigger disabled={disabled} asChild>
        {trigger}
      </DropdownMenuTrigger>
      <DropdownMenuPortal>
        <DropdownMenuContent
          onCloseAutoFocus={(e) => e.preventDefault()}
          align={isRTL ? "end" : "start"}
          sideOffset={2}
          className={cn(
            "oui-deposit-token-select-dropdown-menu-content",
            "oui-bg-base-8 oui-p-1",
            "oui-w-[var(--radix-dropdown-menu-trigger-width)]",
            "oui-rounded-md oui-select-none",
          )}
        >
          <ScrollArea>
            <div className="oui-max-h-[254px]">{content}</div>
          </ScrollArea>
        </DropdownMenuContent>
      </DropdownMenuPortal>
    </DropdownMenuRoot>
  );
};
