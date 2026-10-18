import { useConnectWallet, useSetChain } from "@web3-onboard/react";
import { useMemo } from "react";

export function useEvm() {
  const [
    {
      wallet, // the wallet that has been connected or null if not yet connected
      connecting, // boolean indicating if connection is in progress
    },
    connect, // function to call to initiate user to connect wallet
    evmDisconnect, // function to call with wallet<DisconnectOptions> to disconnect wallet
    updateBalances, // function to be called with an optional array of wallet addresses connected through Onboard to update balance or empty/no params to update all connected wallets
    setWalletModules, // function to be called with an array of wallet modules to conditionally allow connection of wallet types i.e. setWalletModules([ledger, trezor, injected])
    setPrimaryWallet, // function that can set the primary wallet and/or primary account within that wallet. The wallet that is set needs to be passed in for the first parameter and if you would like to set the primary account, the address of that account also needs to be passed in
  ] = useConnectWallet();

  const [
    {
      chains, // the list of chains that web3-onboard was initialized with
      connectedChain: evmConnectChain, // the current chain the user's wallet is connected to
      settingChain, // boolean indicating if the chain is in the process of being set
    },
    setChain, // function to call to initiate user to switch chains in their wallet
  ] = useSetChain();

  const connected = useMemo(() => {
    return !!(wallet && wallet.accounts && wallet.accounts[0] && wallet.accounts[0].address);
  }, [wallet]);

  const disconnect = async () => {
    console.log('-- disconnect evm xxxxxxxxxxxx');
    if (!wallet) {
      return;
    }
    return evmDisconnect({
      label: wallet.label,
    });
  };

  const connectedChain = useMemo(() => {
    return evmConnectChain
      ? { ...evmConnectChain, id: parseInt(evmConnectChain.id) }
      : null;
  }, [evmConnectChain]);

  const changeChain = (chain: { chainId: string}): Promise<any> =>{
    return setChain(chain);
  }

  return {
    connect,
    connected,
    disconnect,
    connecting,
    wallet,
    connectedChain,
    changeChain,

  };
}
