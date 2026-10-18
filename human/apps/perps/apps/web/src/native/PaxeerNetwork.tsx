"use client";
import React, { useState, useEffect } from "react";
import { useRouter } from "next/router";
import { WagmiProvider } from "@paxeer-network/paxeer-backend/walletRuntime";
import {
  QueryClient,
  QueryClientProvider,
} from "@paxeer-network/paxeer-backend/walletRuntime";
import { PaxeerNetworkAppProvider } from "@paxeer-network/react-app";
import {
  WalletConnectorProvider,
  NativeWalletDialog,
} from "@paxeer-network/wallet-connector";
import { Scaffold } from "@paxeer-network/ui-scaffold";
import { TradingPage } from "@paxeer-network/trading-next";
import { registerLayoutSplitPlugin } from "@paxeer-network/layout-split/plugin";
import { registerLayoutGridPlugin } from "@paxeer-network/layout-grid/plugin";
import { MarketsHomePage } from "@paxeer-network/markets";
import {
  PortfolioLayoutWidget,
  OverviewModule,
  PositionsModule,
  OrdersModule,
  AssetsModule,
  SettingModule,
  FeeTierModule,
  APIManagerModule,
  HistoryModule,
} from "@paxeer-network/portfolio";
import {
  TradingActiveIcon,
  TradingInactiveIcon,
  PortfolioActiveIcon,
  PortfolioInactiveIcon,
  MarketsActiveIcon,
  MarketsInactiveIcon,
  PaxeerNetworkPluginProvider,
} from "@paxeer-network/ui";
import { wagmiConfig } from "@paxeer-network/paxeer-backend/core/wagmi";
import { paxeerChain } from "@paxeer-network/paxeer-backend/core/chain";
import {
  PaxeerProvider,
  usePaxeer,
} from "@paxeer-network/paxeer-backend/core/wallet/embedded";
import { nativeChains } from "@paxeer-network/paxeer-backend/nativeMetadata";
import {
  NativeConfigStore,
  erc20Abi,
  DIAMOND_ADDRESS,
  COLLATERAL_TOKENS,
} from "./config";
import { TradingAccountABI } from "@paxeer-network/paxeer-backend/core/network/abi/TradingAccount";
import { BLUE_DARK_THEME_CSS_VARS } from "../theme/blueDarkTheme";
import {
  nativeLayoutBindings,
  registerNativeSplitLayout,
  registerNativeGridLayout,
} from "./layoutBindings";
const configStore = new NativeConfigStore();
const contracts = {
  getContractInfoByEnv: () => ({
    usdcAbi: erc20Abi,
    erc20Abi,
    vaultAbi: TradingAccountABI,
    verifyContractAddress: DIAMOND_ADDRESS,
    usdcAddress: COLLATERAL_TOKENS.usdc.address,
    vaultAddress: DIAMOND_ADDRESS,
    solanaUSDCAddress: "",
    solanaVaultAddress: "",
  }),
};
const splitPlugins = [registerNativeSplitLayout];
const gridPlugins = [registerNativeGridLayout];
const theme = [
  {
    id: "dark",
    name: "Paxeer Xtrades",
    cssVars: {
      ...BLUE_DARK_THEME_CSS_VARS,
      "--oui-color-primary": "255 255 255",
      "--oui-color-primary-light": "255 255 255",
      "--oui-color-primary-darken": "255 255 255",
      "--oui-color-primary-contrast": "0 0 0",
      "--oui-color-link": "238 240 244",
      "--oui-color-link-light": "255 255 255",
      "--oui-gradient-brand-start": "255 255 255",
      "--oui-gradient-brand-end": "214 218 224",
      "--oui-color-base-6": "16 21 30",
      "--oui-color-base-7": "14 18 26",
      "--oui-color-base-8": "10 13 20",
      "--oui-color-base-9": "8 11 17",
      "--oui-color-base-10": "6 9 15",
      "--oui-color-fill-active": "40 44 52",
      "--oui-color-trading-profit": "0 192 135",
      "--oui-color-trading-loss": "255 70 91",
      "--oui-gradient-primary-start": "255 255 255",
      "--oui-gradient-primary-end": "218 222 228",
      "--oui-gradient-secondary-start": "98 104 114",
      "--oui-gradient-secondary-end": "38 42 49",
    },
  },
];
const menus = [
  { name: "Trade", href: "/trade" },
  { name: "Portfolio", href: "/portfolio" },
  { name: "Markets", href: "/markets" },
  { name: "Grid", href: "/trade/grid" },
];
const chart = {
  scriptSRC: "/static/charting_library/charting_library.standalone.js",
  library_path: "/static/charting_library/",
  theme: "dark" as const,
};
export default function PaxeerNetwork() {
  const [queryClient] = useState(
    () =>
      new QueryClient({
        defaultOptions: { queries: { retry: 1, refetchOnWindowFocus: false } },
      }),
  );
  return (
    <WagmiProvider config={wagmiConfig}>
      <QueryClientProvider client={queryClient}>
        <PaxeerProvider>
          <WalletConnectorProvider>
            <NativePages />
          </WalletConnectorProvider>
        </PaxeerProvider>
      </QueryClientProvider>
    </WagmiProvider>
  );
}
function NativePages() {
  const router = useRouter();
  const path = router.asPath.split("?")[0];
  const auth = usePaxeer();
  useEffect(() => {
    if (auth.isAuthenticated && path.startsWith("/auth/callback"))
      void router.replace("/trade");
  }, [auth.isAuthenticated, path, router]);
  const grid = path.startsWith("/trade/grid");
  const portfolio = path.startsWith("/portfolio");
  const markets = path === "/markets";
  const symbol =
    typeof router.query.symbol === "string"
      ? router.query.symbol
      : "PERP_ETH_USDC";
  const navigate = ({ href }: { href: string }) => {
    void router.push(href);
  };
  const navigation = {
    mainMenus: menus,
    initialMenu: portfolio
      ? "/portfolio"
      : markets
        ? "/markets"
        : grid
          ? "/trade/grid"
          : "/trade",
  };
  const bottomNavProps = {
    mainMenus: [
      {
        name: "Trade",
        href: "/trade",
        activeIcon: <TradingActiveIcon />,
        inactiveIcon: <TradingInactiveIcon />,
      },
      {
        name: "Markets",
        href: "/markets",
        activeIcon: <MarketsActiveIcon />,
        inactiveIcon: <MarketsInactiveIcon />,
      },
      {
        name: "Portfolio",
        href: "/portfolio",
        activeIcon: <PortfolioActiveIcon />,
        inactiveIcon: <PortfolioInactiveIcon />,
      },
    ],
    current: portfolio ? "/portfolio" : markets ? "/markets" : "/trade",
    onRouteChange: navigate,
  };
  const scaffold = {
    bottomNavProps,
    mainNavProps: navigation,
    routerAdapter: { onRouteChange: navigate, currentPath: path },
    footerProps: { poweredBy: false, trailing: "Paxeer Xtrades" },
  };
  const providerProps = {
    configStore,
    contracts,
    defaultChain: { id: paxeerChain.id },
    customChains: { mainnet: nativeChains(), testnet: [] },
    onRouteChange: navigate,
    themes: theme,
    appIcons: {
      main: {
        component: (
          <span className="site-wordmark">
            <img src="/brand/paxeer-x.png" alt="" width={32} height={32} />
            <span>Paxeer Xtrades</span>
          </span>
        ),
      },
      secondary: { img: "/brand/paxeer-x.png" },
    },
  };
  let content: React.ReactNode;
  if (markets)
    content = (
      <Scaffold {...scaffold}>
        <MarketsHomePage
          onSymbolChange={(row: any) =>
            void router.push(`/trade?symbol=${row.symbol}`)
          }
        />
      </Scaffold>
    );
  else if (portfolio) {
    let page = <OverviewModule.OverviewPage />;
    if (path.endsWith("/positions")) page = <PositionsModule.PositionsPage />;
    if (path.endsWith("/orders")) page = <OrdersModule.OrdersPage />;
    if (path.endsWith("/assets")) page = <AssetsModule.AssetsPage />;
    if (path.endsWith("/feeTier")) page = <FeeTierModule.FeeTierPage />;
    if (path.endsWith("/apiKey")) page = <APIManagerModule.APIManagerPage />;
    if (path.endsWith("/history")) page = <HistoryModule.HistoryPage />;
    if (path.endsWith("/setting")) page = <SettingModule.SettingPage />;
    content = (
      <PortfolioLayoutWidget {...scaffold} leftSideProps={{ current: path }}>
        {page}
      </PortfolioLayoutWidget>
    );
  } else
    content = (
      <Scaffold {...scaffold}>
        <PaxeerNetworkPluginProvider
          plugins={grid ? gridPlugins : splitPlugins}
        >
          <TradingPage
            symbol={symbol}
            onSymbolChange={(row) =>
              void router.push(
                `${grid ? "/trade/grid" : "/trade"}?symbol=${row.symbol}`,
              )
            }
            tradingViewConfig={chart}
          />
        </PaxeerNetworkPluginProvider>
      </Scaffold>
    );
  return (
    <PaxeerNetworkAppProvider {...(providerProps as any)}>
      {content}
      <NativeWalletDialog />
    </PaxeerNetworkAppProvider>
  );
}
