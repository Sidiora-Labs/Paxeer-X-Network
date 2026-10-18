import React, {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import {
  useEventEmitter,
  WalletConnectorContext,
} from "@paxeer-network/hooks";
import type {
  WalletConnectorContextState,
  WalletState,
} from "@paxeer-network/hooks";
import { WALLET_CONNECT_ABORTED } from "@paxeer-network/ui-connector";
import { ConnectDrawer } from "./components/connectDrawer";
import {
  type WalletConnectErrorPayload,
  type WalletConnectProviderPayload,
  WALLET_CONNECT_ERROR,
  WALLET_CONNECT_PROVIDER_CANCEL,
  WALLET_CONNECT_PROVIDER_START,
  WALLET_CONNECT_WALLET_SELECTED,
} from "./connectEvents";
import { ConnectRequestController } from "./connectRequest";
import { useWallet } from "./hooks/useWallet";
import "./injectUsercenter";
import { clearOAuthConnectIntent } from "./oauthConnectIntent";
import { useWalletConnectorPrivy } from "./provider";
import { WalletConnectType } from "./types";

interface MainProps {
  headerProps?: {
    mobile: React.ReactNode;
  };
}

export const Main: React.FC<React.PropsWithChildren<MainProps>> = (props) => {
  const { headerProps, children } = props;

  const {
    wallet,
    walletType,
    connectedChain,
    setChain,
    namespace,
    onDisconnect,
    restoreConnectorState,
  } = useWallet();

  const { openConnectDrawer, setOpenConnectDrawer, setTargetWalletType } =
    useWalletConnectorPrivy();
  const ee = useEventEmitter();
  const [connecting, setConnecting] = useState(false);
  const controllerRef = useRef<ConnectRequestController>();
  const connectorSnapshotRef = useRef<{
    walletType: WalletConnectType;
    previousConnectorKey?: string;
    previousChainId?: number;
  } | null>(null);
  const restoreConnectorStateRef = useRef(restoreConnectorState);
  restoreConnectorStateRef.current = restoreConnectorState;

  if (!controllerRef.current) {
    controllerRef.current = new ConnectRequestController(setConnecting);
  }

  const rollbackConnector = useCallback((walletType: WalletConnectType) => {
    const snapshot = connectorSnapshotRef.current;
    if (!snapshot || snapshot.walletType !== walletType) {
      return false;
    }

    connectorSnapshotRef.current = null;
    restoreConnectorStateRef.current(
      snapshot.previousConnectorKey,
      snapshot.previousChainId,
    );
    return true;
  }, []);

  useEffect(() => {
    const controller = controllerRef.current!;
    const handleProviderStart = (payload?: WalletConnectProviderPayload) => {
      if (!payload?.walletType) {
        return;
      }
      if (!connectorSnapshotRef.current) {
        connectorSnapshotRef.current = {
          walletType: payload.walletType,
          previousConnectorKey: payload.previousConnectorKey,
          previousChainId: payload.previousChainId,
        };
      } else {
        connectorSnapshotRef.current.walletType = payload.walletType;
      }
      controller.startProvider(payload.walletType);
    };
    const handleProviderCancel = (payload?: WalletConnectProviderPayload) => {
      if (!payload?.walletType) {
        return;
      }
      if (payload.walletType === WalletConnectType.PRIVY) {
        clearOAuthConnectIntent();
      }
      const rolledBack = rollbackConnector(payload.walletType);
      const cancelled = controller.cancelProvider(payload.walletType);
      if (!rolledBack && !cancelled) {
        return;
      }
      ee.emit(WALLET_CONNECT_ABORTED);
      setOpenConnectDrawer(false);
    };
    const handleConnectError = (payload?: WalletConnectErrorPayload) => {
      if (!payload?.walletType) {
        return;
      }
      if (payload.walletType === WalletConnectType.PRIVY) {
        clearOAuthConnectIntent();
      }
      const rolledBack = rollbackConnector(payload.walletType);
      const failed = controller.fail(
        new Error(payload.message || "Failed to connect to the wallet."),
        payload.walletType,
      );
      if (!rolledBack && !failed) {
        return;
      }
      ee.emit(WALLET_CONNECT_ABORTED);
      setOpenConnectDrawer(false);
    };
    const handleWalletSelected = (selectedWallet?: WalletState) => {
      if (!selectedWallet || !controller.hasPendingRequest) {
        return;
      }
      controller.selectWallet(selectedWallet);
      clearOAuthConnectIntent();
      setOpenConnectDrawer(false);
    };
    ee.on(WALLET_CONNECT_PROVIDER_START, handleProviderStart);
    ee.on(WALLET_CONNECT_PROVIDER_CANCEL, handleProviderCancel);
    ee.on(WALLET_CONNECT_ERROR, handleConnectError);
    ee.on(WALLET_CONNECT_WALLET_SELECTED, handleWalletSelected);
    return () => {
      ee.off(WALLET_CONNECT_PROVIDER_START, handleProviderStart);
      ee.off(WALLET_CONNECT_PROVIDER_CANCEL, handleProviderCancel);
      ee.off(WALLET_CONNECT_ERROR, handleConnectError);
      ee.off(WALLET_CONNECT_WALLET_SELECTED, handleWalletSelected);
      const snapshot = connectorSnapshotRef.current;
      if (snapshot && snapshot.walletType !== WalletConnectType.PRIVY) {
        rollbackConnector(snapshot.walletType);
      }
      controller.dispose();
    };
  }, [ee, rollbackConnector, setOpenConnectDrawer]);

  useEffect(() => {
    if (connectorSnapshotRef.current?.walletType === walletType && wallet) {
      connectorSnapshotRef.current = null;
    }

    const controller = controllerRef.current!;
    const hadPendingRequest = controller.hasPendingRequest;
    controller.completeAggregatedWallet(wallet, walletType);
    if (hadPendingRequest && !controller.hasPendingRequest) {
      clearOAuthConnectIntent();
      setOpenConnectDrawer(false);
    }
  }, [setOpenConnectDrawer, wallet, walletType]);

  const connect = useCallback(
    (options?: { autoSelect?: boolean }): Promise<WalletState[]> => {
      const controller = controllerRef.current!;
      const hadPendingRequest = controller.hasPendingRequest;
      const promise = controller.begin({
        baselineWallet: wallet,
        autoSelect: options?.autoSelect,
      });

      if (!options?.autoSelect && !hadPendingRequest) {
        setTargetWalletType(undefined);
        setOpenConnectDrawer(true);
      }

      return promise;
    },
    [setOpenConnectDrawer, setTargetWalletType, wallet],
  );

  const handleConnectDrawerOpenChange = useCallback(
    (open: boolean) => {
      setOpenConnectDrawer(open);
      if (!open) {
        const cancelledWalletType =
          controllerRef.current!.cancelFromDrawerClose();
        const snapshotWalletType = connectorSnapshotRef.current?.walletType;
        const rollbackWalletType =
          cancelledWalletType ??
          (snapshotWalletType !== WalletConnectType.PRIVY
            ? snapshotWalletType
            : undefined);
        if (rollbackWalletType) {
          rollbackConnector(rollbackWalletType);
        }
        if (snapshotWalletType !== WalletConnectType.PRIVY) {
          ee.emit(WALLET_CONNECT_ABORTED);
        }
      }
    },
    [ee, rollbackConnector, setOpenConnectDrawer],
  );

  const memoizedValue = useMemo<WalletConnectorContextState>(
    () => ({
      connect,
      disconnect: onDisconnect,
      connecting,
      wallet,
      setChain,
      connectedChain,
      namespace,
      chains: [],
      settingChain: false,
    }),
    [
      connect,
      connecting,
      setChain,
      onDisconnect,
      connectedChain,
      wallet,
      namespace,
    ],
  );

  return (
    <WalletConnectorContext.Provider value={memoizedValue}>
      <ConnectDrawer
        open={openConnectDrawer}
        onChangeOpen={handleConnectDrawerOpenChange}
        headerProps={headerProps}
      />
      {children}
    </WalletConnectorContext.Provider>
  );
};
