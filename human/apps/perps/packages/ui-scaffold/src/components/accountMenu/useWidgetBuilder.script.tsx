import { useCallback, useEffect } from "react";
import {
  useAccount,
  useChains,
  useEventEmitter,
  useWalletConnector,
} from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { useAppContext } from "@paxeer-network/react-app";
import {
  AccountStatusEnum,
  isWalletChainChangePendingResult,
} from "@paxeer-network/types";
import { modal, toast, useScreen } from "@paxeer-network/ui";
import {
  ChainSelectorDialogId,
  ChainSelectorSheetId,
} from "@paxeer-network/ui-chain-selector";
import {
  useChainChangeValidation,
  useOnboardingModal,
} from "@paxeer-network/ui-connector";

const WALLET_CONNECT_OAUTH_RESUME_RESULT = "wallet:connect-oauth-resume-result";

type WalletConnectOAuthResumeResult = {
  status?: AccountStatusEnum;
  wrongNetwork?: boolean;
  handled?: boolean;
};

export const useAccountMenu = (): any => {
  const { t } = useTranslation();
  const { disconnect, connectedChain } = useWalletConnector();
  const { account, state } = useAccount();
  const ee = useEventEmitter();
  const { connectWallet, disabledConnect, wrongNetwork, setCurrentChainId } =
    useAppContext();

  const [, { findByChainId }] = useChains();

  const { isMobile } = useScreen();
  const { openOnboardingModal, handleAccountStatus } = useOnboardingModal();

  const handleValidatedStatus = useCallback(
    (status: AccountStatusEnum) => {
      if (status < AccountStatusEnum.EnableTrading) {
        handleAccountStatus(status);
      } else {
        toast.success(t("connector.walletConnected"));
      }
    },
    [handleAccountStatus, t],
  );
  const { onChainChangeBefore, onChainChangeAfter } = useChainChangeValidation({
    onAccountValidated: handleValidatedStatus,
  });
  const {
    onChainChangeBefore: onSwitchNetworkBefore,
    onChainChangeAfter: onSwitchNetworkAfter,
  } = useChainChangeValidation({
    onAccountValidated: handleAccountStatus,
  });

  const openCurrentOnboarding = useCallback(
    () => openOnboardingModal(state.status),
    [openOnboardingModal, state.status],
  );

  const switchChain = useCallback(() => {
    const modalId = isMobile ? ChainSelectorSheetId : ChainSelectorDialogId;
    modal
      .show<{ wrongNetwork: boolean }>(modalId, {
        onChainChangeBefore,
        onChainChangeAfter,
      })
      .catch((error) => {
        if (isWalletChainChangePendingResult(error)) {
          return;
        }
        console.log("[switchChain error]", error);
      });
  }, [isMobile, onChainChangeAfter, onChainChangeBefore]);

  useEffect(() => {
    const handleOAuthResumeResult = (
      result?: WalletConnectOAuthResumeResult,
    ) => {
      if (!result || result.handled) {
        return;
      }
      result.handled = true;
      if (result.wrongNetwork) {
        switchChain();
      } else {
        handleAccountStatus(result.status);
      }
    };

    ee.on(WALLET_CONNECT_OAUTH_RESUME_RESULT, handleOAuthResumeResult);
    return () => {
      ee.off(WALLET_CONNECT_OAUTH_RESUME_RESULT, handleOAuthResumeResult);
    };
  }, [ee, handleAccountStatus, switchChain]);

  const connect = async () => {
    const res = await connectWallet();

    if (!res) {
      return;
    }

    if (res.wrongNetwork) {
      switchChain();
    } else {
      handleAccountStatus(res.status);
    }
  };

  const onOpenExplorer = useCallback(() => {
    if (!connectedChain) {
      return;
    }
    const chainInfo = findByChainId(
      connectedChain!.id as number,
      "network_infos",
    );

    if (chainInfo) {
      // @ts-ignore
      const { explorer_base_url } = chainInfo;
      if (explorer_base_url) {
        if (explorer_base_url.endsWith("/")) {
          window.open(`${explorer_base_url}address/${account.address}`);
        } else {
          window.open(`${explorer_base_url}/address/${account.address}`);
        }
      }
    }
  }, [state, connectedChain]);

  const onDisconnect = async () => {
    localStorage.removeItem("paxeerNetwork_link_device");
    await disconnect({
      label: state.connectWallet?.name,
    });
    await account.disconnect();
  };

  const onSwitchNetwork = () => {
    const modalId = isMobile ? ChainSelectorSheetId : ChainSelectorDialogId;
    modal
      .show<{
        wrongNetwork: boolean;
      }>(modalId, {
        bridgeLessOnly: false,
        isWrongNetwork: wrongNetwork,
        onChainChangeBefore: onSwitchNetworkBefore,
        onChainChangeAfter: onSwitchNetworkAfter,
      })
      .then(
        (r: any) => {
          console.log(r?.chainId);
          if (r?.chainId) {
            setCurrentChainId(r?.chainId);
          }
          toast.success(t("connector.networkSwitched"));
        },
        (error) => {
          if (!isWalletChainChangePendingResult(error)) {
            console.log("[switchChain error]", error);
          }
        },
      );
  };

  return {
    address: state.address,
    accountState: state,
    connect,
    onCrateAccount: openCurrentOnboarding,
    onCreatePaxeerNetworkKey: openCurrentOnboarding,
    onOpenExplorer,
    onDisconnect,
    onSwitchNetwork,
    wrongNetwork,
    disabledConnect,
    isMobile,
  } as const;
};

export type AccountMenuProps = ReturnType<typeof useAccountMenu>;
