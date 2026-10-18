import React, { useEffect, useMemo, useState } from "react";
import { useEventEmitter, useStorageChain } from "@paxeer-network/hooks";
import { AbstractChains, ChainNamespace } from "@paxeer-network/types";
import { WALLET_CONNECT_WALLET_SELECTED } from "../../connectEvents";
import { useWallet } from "../../hooks/useWallet";
import { useWalletConnectorPrivy } from "../../provider";
import { useAbstractWallet } from "../../providers/abstractWallet/abstractWalletProvider";
import { useSolanaWallet } from "../../providers/solana/solanaWalletProvider";
import { useWagmiWallet } from "../../providers/wagmi/wagmiWalletProvider";
import { WalletType } from "../../types";
import { StorageChainNotCurrentWalletType } from "../switchNetworkTips";
import { WalletCard } from "../walletCard";
import { AddAbstractWallet } from "./addAbstractWallet";
import { AddEvmWallet } from "./addEvmWallet";
import { AddSolanaWallet } from "./addSolanaWallet";

interface ConnectWallet {
  type: WalletType;
  address: string;
}

export function RenderNonPrivyWallet() {
  const [walletList, setWalletList] = useState<ConnectWallet[]>([]);
  const [addWalletList, setAddWalletList] = useState<WalletType[]>([]);
  const { storageChain } = useStorageChain();
  const ee = useEventEmitter();
  const { connectorWalletType, walletChainTypeConfig } =
    useWalletConnectorPrivy();
  const { wallet: walletInWagmi, isConnected: isConnectedEvm } =
    useWagmiWallet();
  const { wallet: walletInSolana, isConnected: isConnectedSolana } =
    useSolanaWallet();
  const { wallet: walletInAbstract, isConnected: isConnectedAbstract } =
    useAbstractWallet();
  const { namespace, switchWallet } = useWallet();

  const isActive = (walletType: WalletType) => {
    if (namespace === ChainNamespace.evm) {
      if (walletType === WalletType.ABSTRACT) {
        return AbstractChains.has(storageChain?.chainId);
      }
      if (walletType === WalletType.EVM) {
        return !AbstractChains.has(storageChain?.chainId);
      }
      return false;
    }
    if (namespace === ChainNamespace.solana) {
      return walletType === WalletType.SOL;
    }
    return false;
  };

  const currentConnectWalletType: Set<WalletType> = useMemo(() => {
    const temp = new Set<WalletType>();
    walletList.forEach((wallet) => {
      temp.add(wallet.type);
    });
    return temp;
  }, [walletList]);

  useEffect(() => {
    const tempWalletList = [];
    const tempAddWallet = [];
    if (!connectorWalletType.disableWagmi && walletChainTypeConfig.hasEvm) {
      if (isConnectedEvm) {
        tempWalletList.push({
          type: WalletType.EVM,
          address: walletInWagmi?.accounts[0].address,
        });
      } else {
        tempAddWallet.push(WalletType.EVM);
      }
    }
    if (!connectorWalletType.disableSolana && walletChainTypeConfig.hasSol) {
      if (isConnectedSolana) {
        tempWalletList.push({
          type: WalletType.SOL,
          address: walletInSolana?.accounts[0].address,
        });
      } else {
        tempAddWallet.push(WalletType.SOL);
      }
    }
    if (!connectorWalletType.disableAGW && walletChainTypeConfig.hasAbstract) {
      if (isConnectedAbstract) {
        tempWalletList.push({
          type: WalletType.ABSTRACT,
          address: walletInAbstract?.additionalInfo?.AGWAddress,
        });
      } else {
        tempAddWallet.push(WalletType.ABSTRACT);
      }
    }
    setWalletList(tempWalletList);
    setAddWalletList(tempAddWallet);
  }, [
    isConnectedEvm,
    isConnectedSolana,
    isConnectedAbstract,
    walletInWagmi,
    walletInSolana,
    walletInAbstract,
    walletChainTypeConfig,
    connectorWalletType,
  ]);
  return (
    <>
      {walletList.length && (
        <StorageChainNotCurrentWalletType
          currentWalletType={currentConnectWalletType}
        />
      )}
      <div className="oui-flex oui-flex-col oui-gap-5">
        {walletList.map((wallet) => (
          <WalletCard
            key={wallet.type}
            type={wallet.type}
            address={wallet.address}
            isActive={isActive(wallet.type)}
            onActiveChange={() => {
              const selectedWallet =
                wallet.type === WalletType.EVM
                  ? walletInWagmi
                  : wallet.type === WalletType.SOL
                    ? walletInSolana
                    : walletInAbstract;
              if (!selectedWallet) {
                return;
              }
              switchWallet(wallet.type);
              ee.emit(WALLET_CONNECT_WALLET_SELECTED, selectedWallet);
            }}
            isPrivy={false}
            isMulti={walletList.length > 1}
          />
        ))}
      </div>
      <RenderAddWallet addWalletList={addWalletList} />
    </>
  );
}

function RenderAddWallet({ addWalletList }: { addWalletList: WalletType[] }) {
  if (addWalletList.length === 0) {
    return null;
  }
  return (
    <>
      <div className="oui-my-5 oui-h-px oui-bg-line" />
      <div className="oui-flex oui-flex-col oui-gap-5">
        {addWalletList.map((wallet, index) => {
          if (wallet === WalletType.EVM) {
            return <AddEvmWallet key={index} />;
          }
          if (wallet === WalletType.SOL) {
            return <AddSolanaWallet key={index} />;
          }
          if (wallet === WalletType.ABSTRACT) {
            return <AddAbstractWallet key={index} />;
          }
        })}
      </div>
    </>
  );
}
