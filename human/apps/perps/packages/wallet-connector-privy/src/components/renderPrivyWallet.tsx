import React, { useCallback, useEffect, useMemo, useState } from "react";
import {
  useEventEmitter,
  useStorageChain,
  useWalletConnector,
} from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { AbstractChains, ChainNamespace } from "@paxeer-network/types";
import { cn, ExclamationFillIcon } from "@paxeer-network/ui";
import { windowGuard } from "@paxeer-network/utils";
import { WALLET_CONNECT_WALLET_SELECTED } from "../connectEvents";
import { useWallet } from "../hooks/useWallet";
import { useWalletConnectorPrivy } from "../provider";
import { usePrivyWallet } from "../providers/privy/privyWalletProvider";
import { WalletChainTypeEnum, WalletConnectType, WalletType } from "../types";
import { RenderPrivyTypeIcon, EVMChainPopover } from "./common";
import { MoreIcon } from "./icons";
import { WalletCard } from "./walletCard";

function NoWallet() {
  const { walletChainType } = useWalletConnectorPrivy();
  const { t } = useTranslation();
  return (
    <div className="oui-mt-5 oui-flex oui-w-full oui-flex-col oui-items-center oui-justify-center">
      <div className="oui-flex oui-w-full oui-flex-col oui-items-center oui-justify-center oui-gap-3">
        <div className="oui-flex oui-w-full oui-flex-col oui-items-center oui-justify-center oui-gap-1 oui-rounded-[8px] oui-border  oui-border-line-12 oui-px-2 oui-py-[13px] ">
          <img
            src="/assets/static/sdk/privy/no-wallet.png"
            className="oui-size-[64px]"
          />
          <div className="oui-text-2xs oui-font-semibold oui-text-base-contrast-36">
            {t("connector.privy.noWallet")}
          </div>
        </div>
        <div className="oui-flex oui-items-start oui-gap-1 oui-rounded-[4px] oui-bg-[rgba(var(--oui-color-warning-darken)/0.15)] oui-px-2 oui-py-[6px] ">
          <ExclamationFillIcon
            size={10}
            className="oui-mt-1 oui-size-[10px] oui-shrink-0 oui-text-warning-darken"
          />
          <div className="oui-text-2xs oui-text-warning-darken">
            {t("connector.privy.noWallet.description")}
          </div>
        </div>
      </div>
      <div className="oui-my-5 oui-h-px oui-w-full oui-bg-line" />
      <div className="oui-flex oui-w-full oui-flex-col oui-gap-2">
        {walletChainType === WalletChainTypeEnum.EVM_SOL && (
          <>
            <CreateEVMWallet />
            <CreateSOLWallet />
          </>
        )}
        {walletChainType === WalletChainTypeEnum.onlyEVM && <CreateEVMWallet />}
        {walletChainType === WalletChainTypeEnum.onlySOL && <CreateSOLWallet />}
      </div>
    </div>
  );
}

function CreateEVMWallet() {
  const { t } = useTranslation();
  const { createEvmWallet } = usePrivyWallet();
  const [loading, setLoading] = useState(false);
  const doCreate = () => {
    if (loading) {
      return;
    }
    setLoading(true);
    createEvmWallet()
      .then(() => {
        setLoading(false);
      })
      .catch((e: any) => {
        console.warn("createEvmWallet error", e);
        setLoading(false);
      });
  };
  return (
    <div className="oui-w-full oui-rounded-[8px] oui-bg-base-10 oui-px-2 oui-py-[11px]">
      <div className="oui-flex oui-items-center oui-justify-center oui-gap-1 ">
        <div className="oui-flex oui-items-center oui-justify-center oui-gap-1">
          <div className="oui-relative oui-flex oui-w-[55px] oui-items-center oui-justify-start">
            <img
              src="/assets/static/sdk/chains.png"
              className="oui-relative oui-z-0 oui-h-[18px]"
            />
            <div className="oui-absolute oui-end-0 oui-flex oui-size-[18px] oui-items-center oui-justify-center oui-rounded-full oui-bg-base-5">
              <EVMChainPopover>
                <MoreIcon
                  className="oui-relative oui-z-10 oui-size-3 oui-text-base-contrast-54 hover:oui-text-base-contrast"
                  style={{ zIndex: 1 }}
                />
              </EVMChainPopover>
            </div>
          </div>
        </div>
        <div
          className={cn(
            "oui-cursor-pointer oui-text-2xs oui-font-semibold oui-text-base-contrast-80",
            loading && "oui-opacity-50",
          )}
          onClick={doCreate}
        >
          {t("connector.privy.createEvmWallet")}
        </div>
      </div>
    </div>
  );
}

function CreateSOLWallet() {
  const { t } = useTranslation();
  const { createSolanaWallet } = usePrivyWallet();
  const [loading, setLoading] = useState(false);
  const doCreate = () => {
    if (loading) {
      return;
    }
    setLoading(true);
    createSolanaWallet()
      .then(() => {
        setLoading(false);
      })
      .catch((e: any) => {
        console.warn("create solana wallet error", e);
        setLoading(false);
      });
  };
  return (
    <div className="oui-w-full oui-rounded-[8px] oui-bg-base-10 oui-px-2 oui-py-[11px]">
      <div className="oui-flex oui-items-center oui-justify-center oui-gap-1 ">
        <img
          src="/assets/static/sdk/solana-logo.png"
          className="oui-size-[15px]"
        />
        <div
          className={cn(
            "oui-cursor-pointer oui-text-2xs oui-font-semibold oui-text-base-contrast-80",
            loading && "oui-opacity-50",
          )}
          onClick={doCreate}
        >
          {t("connector.privy.createSolanaWallet")}
        </div>
      </div>
    </div>
  );
}
interface ConnectWallet {
  type: WalletType;
  address: string;
}

export function RenderPrivyWallet() {
  const { t } = useTranslation();
  const {
    targetWalletType,
    setTargetWalletType,
    walletChainTypeConfig,
    connectorWalletType,
  } = useWalletConnectorPrivy();
  const ee = useEventEmitter();
  const {
    walletEVM,
    walletSOL,
    allWalletsEVM,
    allWalletsSOL,
    linkedAccount,
    selectWallet,
  } = usePrivyWallet();
  const { switchWallet } = useWallet();
  const { disconnect } = useWalletConnector();
  const { storageChain } = useStorageChain();
  const [walletList, setWalletList] = useState<ConnectWallet[]>([]);
  const [addWallet, setAddWallet] = useState<React.ReactNode[]>([]);
  const [loading, setLoading] = useState(true);

  const isActive = useCallback(
    (walletType: WalletType, address: string) => {
      if (storageChain?.namespace === ChainNamespace.evm) {
        if (walletType === WalletType.EVM) {
          if (AbstractChains.has(storageChain?.chainId)) return false;
          return walletEVM?.accounts[0]?.address === address;
        }
        return false;
      }
      if (storageChain?.namespace === ChainNamespace.solana) {
        if (walletType === WalletType.SOL) {
          return walletSOL?.accounts[0]?.address === address;
        }
        return false;
      }
      return false;
    },
    [storageChain, walletEVM, walletSOL],
  );

  const renderWarning = useCallback(() => {
    let showWarning = false;
    if (AbstractChains.has(storageChain?.chainId)) {
      showWarning = true;
    }
    if (targetWalletType === WalletType.ABSTRACT) {
      showWarning = true;
    }
    if (!showWarning) {
      return;
    }
    return (
      <div className="oui-mt-5 oui-border-b oui-border-line oui-pb-5">
        <div
          className={cn(
            "oui-flex oui-items-start oui-justify-center oui-gap-1",
            "oui-w-full  oui-rounded-[8px] oui-px-2 oui-py-[13px]",
            "oui-bg-warning-darken/10",
          )}
        >
          <ExclamationFillIcon
            size={14}
            className="oui-mt-[2px] oui-size-[14px] oui-shrink-0 oui-text-warning-darken"
          />
          <div className="oui-text-2xs oui-leading-[18px] oui-text-warning-darken">
            Abstract Chain access requires Abstract Global Wallet. Privy
            connection is currently unsupported.
          </div>
        </div>
      </div>
    );
    // }
  }, [targetWalletType]);

  const renderWallet = useCallback(() => {
    if (loading) {
      return;
    }
    if (!walletList.length) {
      return <NoWallet />;
    }
    return (
      <div className="oui-mt-5 oui-flex oui-flex-col oui-gap-5">
        {walletList.map((wallet) => (
          <WalletCard
            key={wallet.address}
            type={wallet.type}
            address={wallet.address}
            isActive={isActive(wallet.type, wallet.address)}
            isPrivy={true}
            isMulti={walletList.length > 1}
            onActiveChange={() => {
              const walletNamespace =
                wallet.type === WalletType.EVM
                  ? ChainNamespace.evm
                  : ChainNamespace.solana;
              const selectedWallet =
                walletNamespace === ChainNamespace.evm
                  ? allWalletsEVM.find(
                      (item) => item.accounts[0]?.address === wallet.address,
                    )
                  : allWalletsSOL.find(
                      (item) => item.accounts[0]?.address === wallet.address,
                    );
              if (!selectedWallet) {
                return;
              }
              setTargetWalletType(wallet.type);
              selectWallet(walletNamespace, wallet.address);
              switchWallet(wallet.type, selectedWallet);
              ee.emit(WALLET_CONNECT_WALLET_SELECTED, selectedWallet);
            }}
          />
        ))}
        {addWallet.map((node, index) => (
          <div key={index}>
            <div className="oui-my-5 oui-h-px oui-w-full oui-bg-line" />
            <div className="oui-flex oui-w-full oui-flex-col oui-gap-2">
              {node}
            </div>
          </div>
        ))}
      </div>
    );
  }, [
    walletList,
    addWallet,
    isActive,
    switchWallet,
    selectWallet,
    allWalletsEVM,
    allWalletsSOL,
    storageChain,
    t,
    loading,
    ee,
  ]);

  useEffect(() => {
    new Promise((resolve) =>
      setTimeout(() => {
        setLoading(false);
        resolve(true);
      }, 200),
    );
  }, []);

  useEffect(() => {
    const tempWalletList: ConnectWallet[] = [];
    const tempAddWallet: React.ReactNode[] = [];
    if (!connectorWalletType.disableWagmi && walletChainTypeConfig.hasEvm) {
      if (allWalletsEVM.length > 0) {
        for (const w of allWalletsEVM) {
          tempWalletList.push({
            type: WalletType.EVM,
            address: w.accounts[0].address,
          });
        }
      } else {
        tempAddWallet.push(<CreateEVMWallet />);
      }
    }
    if (!connectorWalletType.disableSolana && walletChainTypeConfig.hasSol) {
      if (allWalletsSOL.length > 0) {
        for (const w of allWalletsSOL) {
          tempWalletList.push({
            type: WalletType.SOL,
            address: w.accounts[0].address,
          });
        }
      } else {
        tempAddWallet.push(<CreateSOLWallet />);
      }
    }
    setWalletList(tempWalletList);
    setAddWallet(tempAddWallet);
  }, [
    connectorWalletType,
    walletChainTypeConfig,
    allWalletsEVM,
    allWalletsSOL,
  ]);

  useEffect(() => {
    if (targetWalletType === WalletType.ABSTRACT) {
      windowGuard(() => {
        setTimeout(() => {
          setTargetWalletType(undefined);
        }, 5000);
      });
    }
  }, [targetWalletType, setTargetWalletType]);

  return (
    <div className="oui-flex oui-max-h-[70vh] oui-flex-col oui-overflow-y-auto oui-custom-scrollbar">
      <div className="oui-flex oui-items-center oui-justify-between">
        {linkedAccount && (
          <div className="oui-flex oui-items-center oui-justify-start oui-gap-2 oui-text-base-contrast">
            <div>
              <RenderPrivyTypeIcon type={linkedAccount.type} size={24} />
            </div>
            <div className="oui-text-xs">{linkedAccount.address}</div>
          </div>
        )}
        <div
          className="oui-cursor-pointer oui-text-2xs oui-font-semibold oui-text-primary"
          onClick={() =>
            disconnect({ walletType: WalletConnectType.PRIVY } as any)
          }
        >
          {t("connector.privy.logout")}
        </div>
      </div>
      {renderWarning()}
      {renderWallet()}
    </div>
  );
}
