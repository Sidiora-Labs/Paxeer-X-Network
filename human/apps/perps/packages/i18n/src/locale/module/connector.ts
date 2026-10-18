export const connector = {
  "connector.testnet": "Testnet",
  "connector.mainnet": "Mainnet",

  "connector.connect": "Connect",
  "connector.disconnect": "Disconnect",
  "connector.connectWallet": "Connect wallet",
  // "connector.signIn": "Sign in",
  // "connector.signIn.description": "Confirm you are the owner of this wallet",
  "connector.createAccount": "Create account",

  "connector.createAccountWithLedger": "Create account with ledger",
  "connector.enableTradingWithLedger": "Enable trading with ledger",
  "connector.disconnectWallet": "Disconnect wallet",
  "connector.createAccount.description":
    "Confirm wallet ownership to create an account",
  "connector.enableTrading": "Enable trading",
  "connector.enableTrading.description":
    "Enable secure access to our API for lightning fast trading",

  "connector.switchNetwork": "Switch Network",
  "connector.wrongNetwork": "Wrong network",
  "connector.wrongNetwork.tooltip":
    "Please switch to a supported network to continue.",

  "connector.expired":
    "Your previous access has expired, you will receive a signature request to enable trading. Signing is free and will not send a transaction.",

  "connector.rememberMe": "Remember me",
  "connector.rememberMe.description":
    "Toggle this option to skip these steps next time you want to trade.",

  "connector.referralCode.placeholder": "Referral code (Optional)",
  "connector.referralCode.invalid":
    "The referral_code must be 4 to 15 characters long, only accept upper case roman characters and numbers",
  "connector.referralCode.notExist": "This referral code does not exist.",

  "connector.somethingWentWrong": "Something went wrong",
  "connector.userRejected": "User rejected the request.",

  "connector.walletConnected": "Wallet connected",
  "connector.networkSwitched": "Network switched",
  "connector.switchChain.failed": "Switch chain failed",

  "connector.trade.connectWallet.tooltip":
    "Please connect wallet before starting to trade",
  // "connector.trade.signIn.tooltip": "Please sign in before starting to trade",
  "connector.trade.createAccount.tooltip":
    "Please create account before starting to trade",
  "connector.trade.enableTrading.tooltip":
    "Please enable trading before starting to trade",

  "connector.setUp.connectWallet.tooltip":
    "Please connect wallet before set up",
  // "connector.setUp.signIn.tooltip": "Please sign in before set up",
  "connector.setUp.createAccount.tooltip":
    "Please create account before set up",
  "connector.setUp.enableTrading.tooltip":
    "Please enable trading before set up",

  "connector.ledger.signMessageFailed": "Sign Message Failed",
  "connector.ledger.signMessageFailed.description":
    "Are you using Ledger Wallet?",

  "connector.privy.loginIn": "Login in",
  "connector.privy.logout": "Log out",
  "connector.privy.email": "Email",
  "connector.privy.google": "Google",
  "connector.privy.twitter": "X / Twitter",
  "connector.privy.telegram": "Telegram",
  "connector.privy.myWallet": "My wallet",
  "connector.privy.addEvmWallet": "Add Evm wallet",
  "connector.privy.addSolanaWallet": "Add Solana wallet",
  "connector.privy.addAbstractWallet": "Add Abstract wallet",
  "connector.privy.createEvmWallet": "Create Evm wallet",
  "connector.privy.createSolanaWallet": "Create Solana wallet",
  "connector.privy.termsOfUse":
    "By connecting your wallet, you acknowledge and agree to the <0>terms of use</0>.",
  "connector.privy.supportedEvmChain": "Supported Evm chain",
  "connector.privy.supportedSolanaChain": "Supported Solana chain",
  "connector.privy.noWallet": "No wallet",
  "connector.privy.noWallet.description":
    "Please create a wallet to proceed. Only you can access the private key. You can export the private key and import your wallet into another wallet client, such as MetaMask or Phantom, at any time.",
  "connector.privy.switchNetwork.tips": "Switch to {{chainName}} to continue.",
  "connector.privy.addEvmWallet.tips":
    "Connect an EVM-compatible wallet to continue using the EVM network.",
  "connector.privy.addSolanaWallet.tips":
    "Connect an Solana-compatible wallet to continue using the Solana network.",
  "connector.privy.addAbstractWallet.tips":
    "Connect an Abstract-compatible wallet to continue using the Abstract network.",
  "connector.privy.pwa.title": "Prefer an app-like experience? Try the PWA",
  "connector.privy.pwa.sheetTitle": "Add as App",
  "connector.privy.pwa.description":
    "For a better mobile experience, add this website to your home screen as an app from Safari or Chrome.",
  "connector.privy.pwa.step1": "Click the share icon in the browser menu.",
  "connector.privy.pwa.step2": "Choose Add to Home Screen in the options.",
  "connector.privy.pwa.findOnPage": "Find on Page",
  "connector.privy.pwa.addToHomeScreen": "Add to Home Screen",
};

export type Connector = typeof connector;
