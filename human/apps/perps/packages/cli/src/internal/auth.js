const fs = require("fs");
const os = require("os");
const path = require("path");
const { CLI_BIN_NAME, AUTH_API_BASE_URL } = require("./constants");

const authHome =
  process.env.HOME ||
  process.env.USERPROFILE ||
  path.join(os.tmpdir(), ".paxeer-network-cli");
const AUTH_DIR = path.join(authHome, ".paxeer-network");
const AUTH_FILE = path.join(AUTH_DIR, "auth.json");
const DEFAULT_HTTP_TIMEOUT_MS = 15000;
const HTTP_TIMEOUT_MS = Number.parseInt(
  process.env.PAXEER_NETWORK_HTTP_TIMEOUT_MS || `${DEFAULT_HTTP_TIMEOUT_MS}`,
  10,
);

function ensureAuthDir() {
  if (!fs.existsSync(AUTH_DIR)) {
    fs.mkdirSync(AUTH_DIR, { recursive: true });
  }
}

function readAuth() {
  try {
    if (fs.existsSync(AUTH_FILE)) {
      return JSON.parse(fs.readFileSync(AUTH_FILE, "utf-8"));
    }
  } catch (e) {
    // Ignore errors, return empty
  }
  return null;
}

function writeAuth(data) {
  ensureAuthDir();
  fs.writeFileSync(AUTH_FILE, JSON.stringify(data, null, 2), { mode: 0o600 });
  try {
    fs.chmodSync(AUTH_FILE, 0o600);
  } catch {
    // Best-effort on platforms that do not support chmod (e.g. some Windows FS).
  }
}

function saveToken(token, email = null) {
  writeAuth({ token, email });
}

function saveOAuthToken(tokenData) {
  // Use email if available, otherwise use username
  writeAuth({
    token: tokenData.token,
    refreshToken: tokenData.refreshToken || null,
    email: tokenData.email || null,
    username: tokenData.username || null,
    avatarUrl: tokenData.avatarUrl || null,
    loginMethod: "wallet",
    loginAt: new Date().toISOString(),
  });
}

function getToken() {
  const auth = readAuth();
  return auth?.token || null;
}

function getEmail() {
  const auth = readAuth();
  // Prefer email, fall back to username
  return auth?.email || auth?.username || null;
}

function getRefreshToken() {
  const auth = readAuth();
  return auth?.refreshToken || null;
}

/**
 * Build a deterministic timeout budget for CLI HTTP calls.
 * @returns {number}
 */
function getHttpTimeoutMs() {
  if (Number.isFinite(HTTP_TIMEOUT_MS) && HTTP_TIMEOUT_MS > 0) {
    return HTTP_TIMEOUT_MS;
  }
  return DEFAULT_HTTP_TIMEOUT_MS;
}

/**
 * Execute fetch with timeout so CLI does not wait forever on stalled network.
 * @param {string} url
 * @param {RequestInit} init
 * @param {number} timeoutMs
 * @returns {Promise<Response>}
 */
async function fetchWithTimeout(url, init, timeoutMs) {
  const controller = new AbortController();
  const timeoutId = setTimeout(() => {
    controller.abort(`Request timed out after ${timeoutMs}ms`);
  }, timeoutMs);

  try {
    return await fetch(url, {
      ...init,
      signal: controller.signal,
    });
  } finally {
    clearTimeout(timeoutId);
  }
}

/**
 * Rotate CLI tokens using refresh token and persist new credentials.
 * @param {{ onAuthEvent?: (event: string, details?: Record<string, unknown>) => void }} options
 */
async function refreshCliToken(options = {}) {
  const { onAuthEvent } = options;
  const auth = readAuth();
  const refreshToken = auth?.refreshToken;
  if (!refreshToken) {
    onAuthEvent?.("refresh_missing");
    return null;
  }

  try {
    const refreshUrl = `${AUTH_API_BASE_URL}/refresh-cli`;
    onAuthEvent?.("refresh_started", { url: refreshUrl });
    const response = await fetchWithTimeout(
      refreshUrl,
      {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Accept: "application/json",
        },
        body: JSON.stringify({ refreshToken }),
      },
      getHttpTimeoutMs(),
    );

    if (!response.ok) {
      onAuthEvent?.("refresh_failed", { status: response.status });
      return null;
    }

    const data = await response.json();
    if (!data?.accessToken || !data?.refreshToken) {
      onAuthEvent?.("refresh_invalid_payload");
      return null;
    }

    writeAuth({
      ...auth,
      token: data.accessToken,
      refreshToken: data.refreshToken,
      refreshedAt: new Date().toISOString(),
    });
    onAuthEvent?.("refresh_succeeded");
    return data.accessToken;
  } catch (e) {
    onAuthEvent?.("refresh_error", {
      message: e?.message || String(e),
    });
    return null;
  }
}

/**
 * Send authenticated request and retry once after token refresh on 401.
 * @param {{ onAuthEvent?: (event: string, details?: Record<string, unknown>) => void }} options
 */
async function authenticatedFetch(url, init = {}, options = {}) {
  const { onAuthEvent } = options;
  const token = getToken();
  const timeoutMs = getHttpTimeoutMs();
  if (!token) {
    return fetchWithTimeout(url, init, timeoutMs);
  }

  const firstHeaders = new Headers(init.headers || {});
  firstHeaders.set("Authorization", `Bearer ${token}`);
  let response = await fetchWithTimeout(
    url,
    { ...init, headers: firstHeaders },
    timeoutMs,
  );
  if (response.status !== 401) {
    return response;
  }

  onAuthEvent?.("request_unauthorized", { status: response.status });
  const nextToken = await refreshCliToken({ onAuthEvent });
  if (!nextToken) {
    return response;
  }

  const retryHeaders = new Headers(init.headers || {});
  retryHeaders.set("Authorization", `Bearer ${nextToken}`);
  onAuthEvent?.("request_retry_started");
  response = await fetchWithTimeout(
    url,
    { ...init, headers: retryHeaders },
    timeoutMs,
  );
  return response;
}

function isLoggedIn() {
  const token = getToken();
  return !!token;
}

function logout() {
  if (fs.existsSync(AUTH_FILE)) {
    fs.unlinkSync(AUTH_FILE);
  }
}

/**
 * Ensure the user is logged in; sets process.exitCode on failure.
 * @returns {string | null} Bearer token, or null when unauthenticated.
 */
function requireAuth() {
  const { warn, error, info } = require("../shared");

  if (!isLoggedIn()) {
    warn("You are not logged in.");
    info(`Please run '${CLI_BIN_NAME} login' first to authenticate.`);
    process.exitCode = 1;
    return null;
  }

  const token = getToken();
  if (!token) {
    error("Authentication token not found.");
    info(`Please run '${CLI_BIN_NAME} login' again.`);
    process.exitCode = 1;
    return null;
  }

  return token;
}

module.exports = {
  saveToken,
  saveOAuthToken,
  getToken,
  getEmail,
  getRefreshToken,
  isLoggedIn,
  logout,
  authenticatedFetch,
  refreshCliToken,
  fetchWithTimeout,
  getHttpTimeoutMs,
  requireAuth,
};
