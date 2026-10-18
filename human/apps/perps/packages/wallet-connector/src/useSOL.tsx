import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  WalletAdapterNetwork,
  WalletNotReadyError,
  WalletReadyState,
} from "@solana/wallet-adapter-base";
import { useConnection, useWallet } from "@solana/wallet-adapter-react";
import { useWalletModal } from "@solana/wallet-adapter-react-ui";
import {
  useEventEmitter,
  useStorageLedgerAddress,
  WalletState,
} from "@paxeer-network/hooks";
import { ChainNamespace } from "@paxeer-network/types";
import { useScreen } from "@paxeer-network/ui";
import { useSolanaContext } from "./SolanaProvider";
import { SolanaChainIdEnum, SolanaChains } from "./config";

export function useSOL() {
  const [wallet, setWallet] = useState<WalletState | null>(null);
  const { isMobile } = useScreen();
  const { endpoint, network } = useSolanaContext();
  const { setVisible: setModalVisible, visible } = useWalletModal();
  const { syncLedgerAddress } = useStorageLedgerAddress();
  const {
    signMessage,
    signTransaction,
    sendTransaction,
    publicKey,
    wallet: solanaWallet,
    connect: solanaConnect,
    disconnect: solanaDisconnect,
    connecting,
  } = useWallet();

  // 1 for open, 2 for close, null for default
  const selectModalVisibleRef = useRef<boolean>(false);

  const [connected, setConnected] = useState(false);

  const isManual = useRef(false);
  const solanaPromiseRef = useRef<{
    walletSelect: Promise<any> | null;
    connect: Promise<any> | null;
    walletSelectResolve: (value: any) => void;
    walletSelectReject: (value: any) => void;
    connectResolve: (value: any) => void;
    connectReject: (value: any) => void;
  }>({
    walletSelect: null,
    connect: null,
    walletSelectResolve: () => {},
    walletSelectReject: () => {},
    connectReject: () => {},
    connectResolve: () => {},
  });
  const ee = useEventEmitter();

  const initPromiseRef = () => {
    console.log("-- init solana promise");
    solanaPromiseRef.current.walletSelectResolve = () => {};
    solanaPromiseRef.current.walletSelectReject = () => {};
    solanaPromiseRef.current.connectReject = () => {};
    solanaPromiseRef.current.connectReject = () => {};
    solanaPromiseRef.current.connect = null;
    solanaPromiseRef.current.walletSelect = null;
    solanaPromiseRef.current.walletSelect = new Promise((resolve, reject) => {
      solanaPromiseRef.current.walletSelectResolve = resolve;
      solanaPromiseRef.current.walletSelectReject = reject;
    });
    solanaPromiseRef.current.connect = new Promise((resolve, reject) => {
      solanaPromiseRef.current.connectResolve = resolve;
      solanaPromiseRef.current.connectReject = reject;
    });
  };

  const handleSolanaError = useCallback(
    (e: Error) => {
      console.log("solan connect error", e);

      if (e instanceof WalletNotReadyError) {
        if (isMobile) {
          console.log("-- need toast wallet not ready", e);
          ee.emit("wallet:connect-error", {
            message: "Please open the wallet app and use the in-app browser.",
          });
        }
      }
      solanaDisconnect().then();
    },
    [isMobile, ee],
  );

  const connect = async () => {
    initPromiseRef();
    isManual.current = true;
    if (!solanaPromiseRef.current) {
      return;
    }
    if (!solanaWallet) {
      setModalVisible(true);
      selectModalVisibleRef.current = true;
    } else {
      solanaPromiseRef.current.walletSelectResolve(solanaWallet);
      if (!publicKey) {
        try {
          await solanaConnect();
        } catch (e) {
          solanaPromiseRef.current.connectReject(e);
        }
      } else {
        solanaPromiseRef.current.connectResolve({
          userAddress: publicKey.toBase58(),
          signMessage,
          signTransaction,
          sendTransaction,
        });
      }
    }

    console.log("-- connect fn", solanaWallet, publicKey);
    return Promise.all([
      solanaPromiseRef.current.walletSelect,
      solanaPromiseRef.current.connect,
    ])
      .then(
        ([
          wallet,
          { userAddress, signMessage, signTransaction, sendTransaction },
        ]) => {
          // console.log('-- connect sol res',{
          //   wallet,
          //   userAddress, signMessage, sendTransaction
          // });
          const tempWallet = {
            label: wallet.adapter.name,
            icon: "",
            provider: {
              rpcUrl: endpoint,
              network: network,
              signMessage: signMessage,
              signTransaction,
              sendTransaction,
              publicKey,
            },
            accounts: [
              {
                address: userAddress,
              },
            ],
            chains: [
              {
                id: SolanaChains.get(network)!,
                namespace: ChainNamespace.solana,
              },
            ],
          };
          syncLedgerAddress(userAddress, wallet.adapter.name);
          setWallet(tempWallet as WalletState);
          setConnected(true);
          return [tempWallet];
        },
      )
      .catch((e) => {
        console.log("connect solana error", e);
        handleSolanaError(e);
        return Promise.reject(e);
      })
      .finally(() => {
        isManual.current = false;
      });
  };

  const disconnect = async () => {
    console.log("--- discconnect sol");
    await solanaDisconnect();
    setWallet(null);
    setConnected(false);
    return [];
  };

  const connectedChain = useMemo(() => {
    if (!publicKey) {
      return null;
    }
    return {
      id:
        network === WalletAdapterNetwork.Mainnet
          ? SolanaChainIdEnum.MAINNET
          : SolanaChainIdEnum.DEVNET,
      namespace: ChainNamespace.solana,
    };
  }, [publicKey]);

  useEffect(() => {
    if (selectModalVisibleRef.current) {
      if (!visible && !solanaWallet && solanaPromiseRef.current) {
        console.log(
          "-- select modal visible ref",
          selectModalVisibleRef.current,
        );
        console.log("-- use reject solana select modal");
        solanaPromiseRef.current.walletSelectReject("user reject");
        selectModalVisibleRef.current = false;
      } else if (solanaWallet) {
        selectModalVisibleRef.current = false;
      }
    }
  }, [
    visible,
    solanaWallet,
    solanaPromiseRef.current,
    selectModalVisibleRef.current,
  ]);

  useEffect(() => {
    if (!solanaWallet || !publicKey) {
      console.log("--- not connect sol", solanaWallet, publicKey);
      setConnected(false);
      return;
    }
    console.log("-- publick", {
      publicKey: publicKey.toBase58(),
      isManual: isManual.current,
    });

    if (isManual.current) {
      if (solanaPromiseRef.current) {
        solanaPromiseRef.current.connectResolve({
          userAddress: publicKey?.toBase58(),
          signMessage,
          signTransaction,
          sendTransaction,
        });
      }
      return;
    }
    const userAddress = publicKey.toBase58();
    setWallet({
      label: solanaWallet.adapter.name,
      icon: "",
      provider: {
        signMessage: signMessage!,
        signTransaction: signTransaction!,
        sendTransaction: sendTransaction!,
        rpcUrl: endpoint,
        network: network,
        publicKey,
      },
      accounts: [
        {
          address: userAddress,
        },
      ],
      chains: [
        {
          id: SolanaChains.get(network)!,
          namespace: ChainNamespace.solana,
        },
      ],
    });
    setConnected(true);
  }, [
    publicKey,
    solanaWallet,
    signMessage,
    signTransaction,
    isManual,
    sendTransaction,
    endpoint,
    network,
  ]);

  const ledgerAddress = publicKey?.toBase58();
  const ledgerAdapterName = solanaWallet?.adapter.name;

  useEffect(() => {
    if (!ledgerAddress || !ledgerAdapterName) {
      return;
    }

    syncLedgerAddress(ledgerAddress, ledgerAdapterName);
  }, [ledgerAddress, ledgerAdapterName, syncLedgerAddress]);

  useEffect(() => {
    if (!solanaWallet) {
      return;
    }
    console.log("-- public key", publicKey, { isMobile });
    if (
      isMobile &&
      solanaWallet.readyState === WalletReadyState.Loadable &&
      !isManual.current
    ) {
      solanaDisconnect().then();
      return;
    }

    console.log("-- solana refresh auto connect", solanaWallet);

    if (solanaPromiseRef.current) {
      solanaPromiseRef.current.walletSelectResolve(solanaWallet);
    }
  }, [solanaWallet, publicKey, handleSolanaError, isMobile]);

  return {
    connected,
    connect,
    disconnect,
    connecting,
    wallet,
    connectedChain,
  };
}
