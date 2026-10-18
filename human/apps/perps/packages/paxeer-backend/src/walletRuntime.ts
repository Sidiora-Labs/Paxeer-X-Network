/** One native wallet and query context shared by the app and all feature packages. */
export {WagmiProvider,useAccount,useConnect,useDisconnect,useSwitchChain} from "wagmi";
export {reconnect} from "wagmi/actions";
export {QueryClient,QueryClientProvider} from "@tanstack/react-query";
