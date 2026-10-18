import { useCallback, useState } from "react";
import {
  useConfig,
  useAccount,
  useWalletConnector,
} from "@paxeer-network/hooks";
import { useAppContext } from "@paxeer-network/react-app";
import {
  AccountStatusEnum,
  NetworkId,
  type WalletChainChangeState,
} from "@paxeer-network/types";
import { useChainChangeValidation } from "@paxeer-network/ui-connector";

export type UseChainMenuScriptReturn = ReturnType<typeof useChainMenuScript>;

export type UseChainMenuScriptOptions = {
  onAccountValidated?: (status: AccountStatusEnum) => void;
};

export const useChainMenuScript = (options: UseChainMenuScriptOptions = {}) => {
  const [open, setOpen] = useState(false);
  const [loading, setLoading] = useState(false);
  const { state } = useAccount();
  const { connectedChain } = useWalletConnector();
  const { currentChainId, wrongNetwork, disabledConnect, setCurrentChainId } =
    useAppContext();
  const networkId = useConfig("networkId") as NetworkId;
  const {
    onChainChangeBefore: prepareChainChange,
    onChainChangeAfter: completeChainChange,
  } = useChainChangeValidation({
    onAccountValidated: options.onAccountValidated,
  });

  const hide = useCallback(() => {
    setOpen(false);
  }, []);

  const onChainChangeBefore = useCallback(
    (chainId: number) => {
      setLoading(true);
      hide();
      prepareChainChange(chainId);
    },
    [hide, prepareChainChange],
  );

  const onChainChangeAfter = useCallback(
    (_chainId: number, result: WalletChainChangeState) => {
      setLoading(false);
      completeChainChange(_chainId, result);
    },
    [completeChainChange],
  );

  return {
    isConnected: !!connectedChain,
    currentChainId,
    wrongNetwork,
    disabledConnect,
    accountStatus: state.status,
    networkId,
    open,
    onOpenChange: setOpen,
    hide,
    onChainChangeBefore,
    onChainChangeAfter,
    loading,
    setCurrentChainId,
  };
};

export type UseChainMenuBuilderScript = ReturnType<typeof useChainMenuScript>;
