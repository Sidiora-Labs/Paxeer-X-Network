import { FC, ReactNode, SVGProps, useMemo, useState } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import {
  cn,
  DropdownMenuContent,
  DropdownMenuPortal,
  DropdownMenuRoot,
  DropdownMenuTrigger,
  Flex,
  Text,
  WalletIcon,
  CaretDownIcon,
  CaretUpIcon,
  useDocumentDirection,
} from "@paxeer-network/ui";

const AddIcon: FC<SVGProps<SVGSVGElement>> = (props) => (
  <svg
    width="18"
    height="18"
    viewBox="0 0 18 18"
    fill="currentColor"
    xmlns="http://www.w3.org/2000/svg"
    {...props}
  >
    <path d="M8.99316 2.19731C8.57916 2.19731 8.24316 2.53331 8.24316 2.94731V8.19731H2.99316C2.57916 8.19731 2.24316 8.53331 2.24316 8.94731C2.24316 9.36131 2.57916 9.69731 2.99316 9.69731H8.24316V14.9473C8.24316 15.3613 8.57916 15.6973 8.99316 15.6973C9.40716 15.6973 9.74316 15.3613 9.74316 14.9473V9.69731H14.9932C15.4072 9.69731 15.7432 9.36131 15.7432 8.94731C15.7432 8.53331 15.4072 8.19731 14.9932 8.19731H9.74316V2.94731C9.74316 2.53331 9.40716 2.19731 8.99316 2.19731Z" />
  </svg>
);

export type WalletOption = {
  address: string;
  name?: string;
  network?: "EVM" | "SOL";
};

interface WalletMenuItemProps {
  onClick: () => void;
  children: ReactNode;
  className?: string;
}

const WalletMenuItem: FC<WalletMenuItemProps> = ({
  onClick,
  children,
  className,
}) => {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "oui-flex oui-items-center oui-gap-2 oui-py-1.5 oui-px-2 hover:oui-bg-base-5",
        "oui-rounded-[4px]",
        "oui-font-semibold oui-tracking-[0.03em]",
        className,
      )}
    >
      {children}
    </button>
  );
};

interface WalletSelectorProps {
  connectedWallet?: {
    name: string;
    address: string;
    namespace?: string;
  };
  externalWallets: WalletOption[];
  selectedAddress: string;
  onSelect: (address: string) => void;
  onAddExternalWallet: () => void;
}

export const WalletSelector: FC<WalletSelectorProps> = ({
  connectedWallet,
  externalWallets,
  selectedAddress,
  onSelect,
  onAddExternalWallet,
}) => {
  const { t } = useTranslation();
  const [isOpen, setIsOpen] = useState(false);
  const dir = useDocumentDirection();
  const isRTL = dir === "rtl";

  const currentNetwork = useMemo<"EVM" | "SOL" | undefined>(() => {
    if (!connectedWallet?.namespace) return undefined;
    const ns = connectedWallet.namespace.toLowerCase();
    if (ns.includes("sol")) return "SOL";
    return "EVM";
  }, [connectedWallet?.namespace]);

  const filteredExternalWallets = useMemo(() => {
    if (!currentNetwork) return externalWallets;
    return externalWallets.filter(
      (wallet) => wallet.network === currentNetwork,
    );
  }, [externalWallets, currentNetwork]);

  const selectedWalletOpt = useMemo(() => {
    if (connectedWallet && selectedAddress === connectedWallet.address) {
      return {
        address: connectedWallet.address,
        name: connectedWallet.name,
        network: currentNetwork,
      };
    }
    return filteredExternalWallets.find(
      (w: WalletOption) => w.address === selectedAddress,
    );
  }, [
    connectedWallet,
    currentNetwork,
    filteredExternalWallets,
    selectedAddress,
  ]);

  const hasExternalWallets = filteredExternalWallets.length > 0;
  const showConnectedItem = !!connectedWallet && hasExternalWallets;

  const getChainLabel = (network?: "EVM" | "SOL") => {
    if (!network) return "";
    if (network === "SOL") return "Solana";
    return "EVM";
  };

  const handleSelectWallet = (address: string) => {
    onSelect(address);
    setIsOpen(false);
  };

  const handleAddExternalWalletClick = () => {
    onAddExternalWallet();
    setIsOpen(false);
  };

  const dropdownContent = (
    <DropdownMenuPortal>
      <DropdownMenuContent
        align={isRTL ? "start" : "end"}
        className="oui-max-h-[240px] oui-overflow-y-auto oui-custom-scrollbar"
      >
        {showConnectedItem && connectedWallet && (
          <WalletMenuItem
            onClick={() => handleSelectWallet(connectedWallet.address)}
            className="oui-w-[368px]"
          >
            <Flex direction="column" itemAlign="start" gapY={1}>
              <Text size="2xs" className="oui-text-base-contrast-54">
                {t("common.myWallet")}
              </Text>
              <Text
                size="2xs"
                intensity={54}
                className="oui-text-base-contrast-36 oui-leading-[15px]"
              >
                {`(${getChainLabel(currentNetwork)}) ${connectedWallet.address}`}
              </Text>
            </Flex>
          </WalletMenuItem>
        )}

        {hasExternalWallets && (
          <>
            {filteredExternalWallets.map((wallet) => (
              <WalletMenuItem
                key={wallet.address}
                onClick={() => handleSelectWallet(wallet.address)}
                className="oui-w-[368px]"
              >
                <Flex direction="column" itemAlign="start" gapY={1}>
                  <Text size="2xs" className="oui-text-base-contrast-54">
                    {t("common.externalWallet")}
                  </Text>
                  <Text
                    size="2xs"
                    intensity={54}
                    className="oui-text-base-contrast-36 oui-leading-[15px]"
                  >
                    {`(${getChainLabel(wallet.network)}) ${wallet.address}`}
                  </Text>
                </Flex>
              </WalletMenuItem>
            ))}
          </>
        )}

        <WalletMenuItem
          onClick={handleAddExternalWalletClick}
          className={cn(
            "oui-justify-center",
            hasExternalWallets && "oui-w-full",
            !hasExternalWallets && "oui-w-[190px]",
          )}
        >
          <Flex
            direction={isRTL ? "rowReverse" : "row"}
            itemAlign="center"
            gapX={1}
            className="oui-text-primary oui-font-semibold oui-tracking-[0.03em]"
          >
            <AddIcon className="oui-size-[18px]" />
            <Text size="2xs">{t("transfer.withdraw.addExternalWallet")}</Text>
          </Flex>
        </WalletMenuItem>
      </DropdownMenuContent>
    </DropdownMenuPortal>
  );

  return (
    <Flex justify="between" className="oui-w-full oui-mb-3">
      <Text size="2xs" intensity={36}>
        {t("common.wallet")}
      </Text>
      <DropdownMenuRoot open={isOpen} onOpenChange={setIsOpen}>
        <DropdownMenuTrigger asChild>
          <div
            className={cn(
              "oui-flex oui-items-center oui-gap-1 oui-cursor-pointer oui-text-base-contrast-54 hover:oui-text-base-contrast-80",
              isRTL && "oui-flex-row-reverse",
            )}
          >
            {selectedWalletOpt?.name && (
              <WalletIcon name={selectedWalletOpt.name} size="3xs" />
            )}
            <Text.formatted size="2xs" intensity={54} rule="address">
              {selectedWalletOpt?.address || ""}
            </Text.formatted>
            <Text
              size="2xs"
              intensity={54}
              className="oui-text-base-contrast-36"
            >
              {` (${getChainLabel(selectedWalletOpt?.network)})`}
            </Text>
            {isOpen ? (
              <CaretUpIcon size={12} className="oui-text-inherit" />
            ) : (
              <CaretDownIcon size={12} className="oui-text-inherit" />
            )}
          </div>
        </DropdownMenuTrigger>
        {dropdownContent}
      </DropdownMenuRoot>
    </Flex>
  );
};
