/** Published CLI binary name (see package.json bin field). */
const CLI_BIN_NAME = "paxeer-network-devkit";

// Known interceptor targets from the plugin system
const INTERCEPTOR_TARGETS = [
  "Trading.Layout.Desktop",
  "Trading.Layout.Mobile",
  "Trading.OrderEntry.TypeTabs",
  "Trading.OrderEntry.BuySellSwitch",
  "Trading.OrderEntry.Available",
  "Trading.OrderEntry.QuantitySlider",
  "Trading.OrderEntry.SubmitSection",
  "OrderBook.Desktop.Asks",
  "OrderBook.Desktop.Bids",
  "Deposit.DepositForm",
  "Deposit.WithdrawForm",
  "Account.AccountMenu",
  "Layout.MainMenus",
  "Table.EmptyDataIdentifier",
];

// Module types supported
const MODULE_TYPES = ["page", "component", "hook", "utils", "module"];

/**
 * Reduce PAXEER_NETWORK_API_URL to the origin the CLI appends API paths to. Before
 * the dex.paxeer-network.network migration this variable held a full API base, so an
 * existing `/api`, `/api/marketplace`, or `/api/auth` suffix is stripped
 * instead of being duplicated.
 * @param {string | undefined} value
 * @returns {string | null}
 */
function toApiOrigin(value) {
  const trimmed = String(value || "")
    .trim()
    .replace(/\/+$/, "");
  if (!trimmed) {
    return null;
  }
  return trimmed.replace(/\/api(?:\/marketplace|\/auth)?$/, "");
}

// New marketplace (paxeer-network One / dex-creator) endpoints. Auth routes live
// under /api/auth while marketplace routes live under /api/marketplace, so
// the two bases must be derived from the same origin separately.
const MARKETPLACE_API_ORIGIN =
  toApiOrigin(process.env.PAXEER_NETWORK_API_URL) || "https://dex-api.paxeer-network.network";
const MARKETPLACE_API_BASE_URL = `${MARKETPLACE_API_ORIGIN}/api/marketplace`;
const AUTH_API_BASE_URL = `${MARKETPLACE_API_ORIGIN}/api/auth`;

const MARKETPLACE_WEB_BASE_URL = (
  process.env.PAXEER_NETWORK_WEB_URL || "https://dex.paxeer-network.network"
).replace(/\/+$/, "");
// The web app forces a $lang prefix; the CLI always uses the default locale.
const MARKETPLACE_WEB_LOGIN_URL = `${MARKETPLACE_WEB_BASE_URL}/en/cli/login`;
const MARKETPLACE_WEB_BROWSE_URL = `${MARKETPLACE_WEB_BASE_URL}/en/marketplace`;
/** Owner-facing page for managing submitted plugins and submissions. */
const MARKETPLACE_WEB_LISTINGS_URL = `${MARKETPLACE_WEB_BASE_URL}/en/marketplace-listings`;

const MARKETPLACE_API_PLUGINS_URL = `${MARKETPLACE_API_BASE_URL}/plugins`;
const MARKETPLACE_API_PLUGINS_SUBMIT_URL = `${MARKETPLACE_API_PLUGINS_URL}/submit`;
const MARKETPLACE_API_MY_PLUGINS_URL = `${MARKETPLACE_API_BASE_URL}/my-plugins`;
/**
 * Build plugin detail endpoint for plugin-level operations.
 * @param {string} pluginId
 * @returns {string}
 */
function getMarketplaceApiPluginUrl(pluginId) {
  return `${MARKETPLACE_API_PLUGINS_URL}/${encodeURIComponent(pluginId)}`;
}

const CLI_CALLBACK_PORT = 9876;
const CLI_LOGIN_TIMEOUT_MS = 3 * 60 * 1000; // 3 minutes

/** GitHub shorthand for https://github.com/PaxeerNetworkNetwork/paxeer-network-skills (Vercel skills CLI). */
const PAXEER_NETWORK_SKILLS_REPO = "PaxeerNetworkNetwork/paxeer-network-skills";

/** Default skill IDs from paxeer-network-skills README (install all four non-interactively). */
const PAXEER_NETWORK_PLUGIN_SKILL_NAMES = [
  "paxeer-network-plugin-create",
  "paxeer-network-plugin-write",
  "paxeer-network-plugin-add",
  "paxeer-network-plugin-submit",
];

/** Default MCP server key in mcpServers (matches @paxeer-network/sdk-docs install --name default). */
const DEFAULT_PAXEER_NETWORK_SDK_DOCS_MCP_NAME = "paxeer-network-sdk-docs";

module.exports = {
  CLI_BIN_NAME,
  INTERCEPTOR_TARGETS,
  MODULE_TYPES,
  MARKETPLACE_API_ORIGIN,
  MARKETPLACE_API_BASE_URL,
  AUTH_API_BASE_URL,
  MARKETPLACE_WEB_BASE_URL,
  MARKETPLACE_WEB_LOGIN_URL,
  MARKETPLACE_WEB_BROWSE_URL,
  MARKETPLACE_WEB_LISTINGS_URL,
  MARKETPLACE_API_PLUGINS_URL,
  MARKETPLACE_API_PLUGINS_SUBMIT_URL,
  MARKETPLACE_API_MY_PLUGINS_URL,
  getMarketplaceApiPluginUrl,
  CLI_CALLBACK_PORT,
  CLI_LOGIN_TIMEOUT_MS,
  PAXEER_NETWORK_SKILLS_REPO,
  PAXEER_NETWORK_PLUGIN_SKILL_NAMES,
  DEFAULT_PAXEER_NETWORK_SDK_DOCS_MCP_NAME,
};
