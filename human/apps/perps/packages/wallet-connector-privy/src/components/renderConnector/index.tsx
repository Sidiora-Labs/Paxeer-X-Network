import React from "react";
import { useStorageChain } from "@paxeer-network/hooks";
import { cn, Flex, ScrollArea } from "@paxeer-network/ui";
import { useWallet } from "../../hooks/useWallet";
import { useWalletConnectorPrivy } from "../../provider";
import { WalletConnectType, WalletType } from "../../types";
import { ConnectProps } from "../../types";
import { getChainType } from "../../util";
import { AbstractConnectArea } from "./abstractConnector";
import { PrivyConnectArea } from "./privyConnector";
import { SOLConnectArea } from "./solanaConnector";
import { EVMConnectArea } from "./wagmiConnector";

export function RenderConnector() {
  const { connect } = useWallet();
  const {
    setOpenConnectDrawer,
    connectorWalletType,
    walletChainTypeConfig,
    targetWalletType,
  } = useWalletConnectorPrivy();
  const { storageChain } = useStorageChain();

  const selectedWalletType: WalletType | undefined = (() => {
    if (targetWalletType) return targetWalletType;
    if (!storageChain?.chainId) return undefined;
    try {
      return getChainType(parseInt(storageChain.chainId as string));
    } catch {
      return undefined;
    }
  })();

  const handleConnect = (params: ConnectProps) => {
    connect(params);
    if (params.walletType === WalletConnectType.PRIVY) {
      setOpenConnectDrawer(false);
    }
  };
  const renderPrivyConnectArea = () => {
    if (connectorWalletType.disablePrivy) {
      return null;
    }
    return (
      <PrivyConnectArea
        connect={(type) =>
          handleConnect({
            walletType: WalletConnectType.PRIVY,
            extraType: type,
          })
        }
      />
    );
  };
  const renderWagmiConnectArea = () => {
    if (connectorWalletType.disableWagmi) {
      return null;
    }
    if (!walletChainTypeConfig.hasEvm) {
      return null;
    }
    return (
      <EVMConnectArea
        connect={(connector) =>
          handleConnect({
            walletType: WalletConnectType.EVM,
            connector: connector,
          })
        }
      />
    );
  };
  const renderSolanaConnectArea = () => {
    if (connectorWalletType.disableSolana) {
      return null;
    }
    if (!walletChainTypeConfig.hasSol) {
      return null;
    }

    return (
      <SOLConnectArea
        connect={(walletAdapter) =>
          handleConnect({
            walletType: WalletConnectType.SOL,
            walletAdapter: walletAdapter,
          })
        }
      />
    );
  };
  const renderAbstractConnectArea = () => {
    if (connectorWalletType.disableAGW) {
      return null;
    }
    if (!walletChainTypeConfig.hasAbstract) {
      return null;
    }
    return (
      <AbstractConnectArea
        connect={() =>
          handleConnect({ walletType: WalletConnectType.ABSTRACT })
        }
      />
    );
  };

  const walletOrder = ["evm", "sol", "abstract"] as const;

  const typeToKey: Record<WalletType, (typeof walletOrder)[number]> = {
    [WalletType.EVM]: "evm",
    [WalletType.SOL]: "sol",
    [WalletType.ABSTRACT]: "abstract",
  };

  const prioritizedKey = selectedWalletType
    ? typeToKey[selectedWalletType]
    : undefined;

  const orderedWalletKeys = prioritizedKey
    ? ([
        prioritizedKey,
        ...walletOrder.filter((k) => k !== prioritizedKey),
      ] as const)
    : walletOrder;

  const renderByKey = (key: (typeof walletOrder)[number]) => {
    switch (key) {
      case "evm":
        return renderWagmiConnectArea();
      case "sol":
        return renderSolanaConnectArea();
      case "abstract":
        return renderAbstractConnectArea();
      default:
        return null;
    }
  };
  return (
    <ScrollArea className="oui-flex oui-grow oui-shrik oui-basis-auto oui-custom-scrollbar">
      <div className={cn("oui-flex oui-flex-col oui-gap-4", "md:oui-gap-5")}>
        {renderPrivyConnectArea()}
        {orderedWalletKeys.map((key) => (
          <React.Fragment key={key}>{renderByKey(key)}</React.Fragment>
        ))}
      </div>
    </ScrollArea>
  );
}
